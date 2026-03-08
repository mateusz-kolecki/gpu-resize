// Package resizer provides the primary Processor implementations:
//   - GPUProcessor  – uses OpenCL for hardware-accelerated resizing (GPU or CPU OpenCL device)
//   - CPUProcessor  – pure-Go fallback using golang.org/x/image
//
// The two processors implement the internal/processor.Processor interface so
// that the caller (Wails app or tests) can swap them transparently.
package resizer

import (
	"context"
	"fmt"
	"image"
	"math"
	"runtime"
	"strings"
	"sync"

	"github.com/mateusz/gpu-resize/internal/imageio"
	cl "github.com/mateusz/gpu-resize/internal/opencl"
	"github.com/mateusz/gpu-resize/internal/processor"
	"github.com/mateusz/gpu-resize/internal/turbojpeg"
	"golang.org/x/image/draw"
)

// ---- GPUProcessor -----------------------------------------------------------

// GPUProcessor resizes images using an OpenCL GPU (or OpenCL CPU device when
// no GPU is available). It falls back to CPUProcessor if OpenCL is
// unavailable.
//
// The OpenCL Context is safe for concurrent use: each call to ResizeFile
// creates its own command queue and clones the kernels, so multiple goroutines
// drive the GPU simultaneously with no mutex bottleneck.
type GPUProcessor struct {
	clCtx *cl.Context
	cpuFB *CPUProcessor
}

// NewGPUProcessor initialises the OpenCL context.
// If OpenCL is unavailable it returns a CPUProcessor wrapped as a
// GPUProcessor so callers never need to branch.
func NewGPUProcessor() (*GPUProcessor, error) {
	dev, err := cl.PickBestDevice()
	if err != nil {
		// No OpenCL platform – use CPU fallback transparently.
		return &GPUProcessor{cpuFB: NewCPUProcessor()}, nil
	}

	clCtx, err := cl.NewContext(dev)
	if err != nil {
		return &GPUProcessor{cpuFB: NewCPUProcessor()}, nil
	}

	return &GPUProcessor{clCtx: clCtx}, nil
}

func (g *GPUProcessor) Name() string {
	if g.clCtx != nil {
		return fmt.Sprintf("GPU/OpenCL (%s)", g.clCtx.DeviceName())
	}
	return "CPU (no OpenCL)"
}

func (g *GPUProcessor) Available() bool { return true }

func (g *GPUProcessor) ResizeFile(ctx context.Context, inputPath string, opts processor.Options) (processor.FileResult, error) {
	if g.clCtx == nil {
		return g.cpuFB.ResizeFile(ctx, inputPath, opts)
	}

	applyDefaults(&opts)

	// For JPEG inputs use libjpeg-turbo: decode directly to RGBA (no YCbCr
	// intermediate, no convertYCbCr loop) and encode the output with SIMD.
	// PNG and BMP fall back to the standard imageio path.
	isJPEG := isJPEGPath(inputPath)

	var src image.Image
	if isJPEG {
		pix, w, h, err := turbojpeg.DecodeRGBA(inputPath)
		if err != nil {
			return processor.FileResult{InputPath: inputPath, Err: err}, err
		}
		img := &image.RGBA{Pix: pix, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
		src = img
	} else {
		var err error
		src, err = imageio.Decode(inputPath)
		if err != nil {
			return processor.FileResult{InputPath: inputPath, Err: err}, err
		}
	}

	dstW, dstH := targetDimensions(src.Bounds(), opts)

	// No mutex: each ResizeBilinear/ResizeLanczos call owns its own OpenCL
	// command queue and cloned kernels, so they run fully in parallel.
	var (
		resized image.Image
		err     error
	)
	switch opts.Algorithm {
	case processor.AlgorithmBilinear:
		resized, err = g.clCtx.ResizeBilinear(src, dstW, dstH)
	default:
		resized, err = g.clCtx.ResizeLanczos(src, dstW, dstH)
	}

	if err != nil {
		return processor.FileResult{InputPath: inputPath, Err: err}, err
	}

	outPath := imageio.OutputPath(inputPath, opts.OutputDir)

	// Encode: use turbojpeg for JPEG output (fast SIMD encode).
	// readResult always returns *image.RGBA so we can access Pix directly.
	if rgba, ok := resized.(*image.RGBA); ok {
		if err := turbojpeg.EncodeRGBA(outPath, rgba.Pix, rgba.Bounds().Dx(), rgba.Bounds().Dy(), opts.JPEGQuality); err != nil {
			return processor.FileResult{InputPath: inputPath, Err: err}, err
		}
	} else {
		if err := imageio.EncodeJPEG(outPath, resized, opts.JPEGQuality); err != nil {
			return processor.FileResult{InputPath: inputPath, Err: err}, err
		}
	}

	return processor.FileResult{InputPath: inputPath, OutputPath: outPath}, nil
}

func (g *GPUProcessor) ResizeDir(ctx context.Context, inputDir string, opts processor.Options) (<-chan processor.Progress, error) {
	return resizeDir(ctx, g, inputDir, opts)
}

func (g *GPUProcessor) Close() error {
	if g.clCtx != nil {
		g.clCtx.Close()
	}
	return nil
}

// ---- CPUProcessor -----------------------------------------------------------

// CPUProcessor resizes images using the Go standard library / x/image. It is
// used as a fallback when OpenCL is unavailable and is always available.
type CPUProcessor struct{}

// NewCPUProcessor returns a CPUProcessor.
func NewCPUProcessor() *CPUProcessor { return &CPUProcessor{} }

func (c *CPUProcessor) Name() string    { return "CPU (pure-Go)" }
func (c *CPUProcessor) Available() bool { return true }

func (c *CPUProcessor) ResizeFile(ctx context.Context, inputPath string, opts processor.Options) (processor.FileResult, error) {
	applyDefaults(&opts)

	src, err := imageio.Decode(inputPath)
	if err != nil {
		return processor.FileResult{InputPath: inputPath, Err: err}, err
	}

	dstW, dstH := targetDimensions(src.Bounds(), opts)
	resized := cpuResize(src, dstW, dstH, opts.Algorithm)

	outPath := imageio.OutputPath(inputPath, opts.OutputDir)
	if err := imageio.EncodeJPEG(outPath, resized, opts.JPEGQuality); err != nil {
		return processor.FileResult{InputPath: inputPath, Err: err}, err
	}

	return processor.FileResult{InputPath: inputPath, OutputPath: outPath}, nil
}

func (c *CPUProcessor) ResizeDir(ctx context.Context, inputDir string, opts processor.Options) (<-chan processor.Progress, error) {
	return resizeDir(ctx, c, inputDir, opts)
}

func (c *CPUProcessor) Close() error { return nil }

// ---- shared helpers ---------------------------------------------------------

// resizeDir implements the directory batch logic shared by both processors.
func resizeDir(ctx context.Context, p processor.Processor, inputDir string, opts processor.Options) (<-chan processor.Progress, error) {
	files, err := imageio.ScanDir(inputDir)
	if err != nil {
		return nil, err
	}

	applyDefaults(&opts)
	concurrency := opts.MaxConcurrency
	if concurrency <= 0 {
		// For GPU processors we want many jobs in-flight simultaneously so the
		// GPU is always fed: decode/encode on CPU overlaps with kernel execution
		// on the GPU. 4× CPU cores is a good heuristic; CPUProcessor will also
		// benefit from I/O overlapping compute.
		concurrency = runtime.NumCPU() * 4
	}

	ch := make(chan processor.Progress, len(files)+1)

	go func() {
		defer close(ch)

		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		done := 0
		var mu sync.Mutex

		for _, f := range files {
			select {
			case <-ctx.Done():
				return
			default:
			}

			wg.Add(1)
			sem <- struct{}{}

			go func(path string) {
				defer wg.Done()
				defer func() { <-sem }()

				mu.Lock()
				ch <- processor.Progress{Done: done, Total: len(files), Current: path}
				mu.Unlock()

				res, _ := p.ResizeFile(ctx, path, opts)

				mu.Lock()
				done++
				ch <- processor.Progress{Done: done, Total: len(files), Current: path, Result: &res}
				mu.Unlock()
			}(f)
		}

		wg.Wait()
	}()

	return ch, nil
}

// targetDimensions computes output width and height respecting aspect-ratio
// preservation and the configured max dimensions.
func targetDimensions(bounds image.Rectangle, opts processor.Options) (w, h int) {
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	tw := opts.TargetWidth
	th := opts.TargetHeight

	if tw <= 0 && th <= 0 {
		return srcW, srcH
	}

	if !opts.PreserveAspect {
		if tw <= 0 {
			tw = srcW
		}
		if th <= 0 {
			th = srcH
		}
		return tw, th
	}

	// Compute scale factors for each dimension; use the smaller one so the
	// image fits within the target box.
	scaleW := math.MaxFloat64
	scaleH := math.MaxFloat64
	if tw > 0 {
		scaleW = float64(tw) / float64(srcW)
	}
	if th > 0 {
		scaleH = float64(th) / float64(srcH)
	}

	scale := math.Min(scaleW, scaleH)
	// Never upscale – only shrink.
	if scale > 1 {
		scale = 1
	}

	w = int(math.Round(float64(srcW) * scale))
	h = int(math.Round(float64(srcH) * scale))
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return
}

// cpuResize resizes src to dstW×dstH using the pure-Go x/image kernels.
func cpuResize(src image.Image, dstW, dstH int, algo processor.Algorithm) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	var kernel draw.Interpolator
	switch algo {
	case processor.AlgorithmBilinear:
		kernel = draw.BiLinear
	default:
		kernel = draw.CatmullRom // closest to Lanczos in x/image
	}
	kernel.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

// applyDefaults fills in zero values in opts with sensible defaults.
func applyDefaults(opts *processor.Options) {
	if opts.Algorithm == "" {
		opts.Algorithm = processor.AlgorithmLanczos
	}
	if opts.JPEGQuality == 0 {
		opts.JPEGQuality = 90
	}
	if opts.TargetWidth == 0 && opts.TargetHeight == 0 {
		opts.TargetWidth = 1920
		opts.TargetHeight = 1080
	}
}

// isJPEGPath returns true when path has a .jpg or .jpeg extension.
func isJPEGPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg")
}

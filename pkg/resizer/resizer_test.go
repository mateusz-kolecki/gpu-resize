package resizer_test

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mateusz/gpu-resize/internal/processor"
	"github.com/mateusz/gpu-resize/pkg/resizer"
)

// makeJPEG writes a JPEG file of size w×h filled with colour c.
func makeJPEG(t *testing.T, path string, w, h int, c color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
}

func makePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	f, _ := os.Create(path)
	defer f.Close()
	_ = png.Encode(f, img)
}

// ---- CPUProcessor -----------------------------------------------------------

func TestCPUProcessor_Name(t *testing.T) {
	p := resizer.NewCPUProcessor()
	if p.Name() == "" {
		t.Error("Name() should not be empty")
	}
}

func TestCPUProcessor_Available(t *testing.T) {
	p := resizer.NewCPUProcessor()
	if !p.Available() {
		t.Error("CPUProcessor must always be available")
	}
}

func TestCPUProcessor_ResizeFile_Lanczos(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "input.jpg")
	makeJPEG(t, src, 400, 300, color.RGBA{200, 100, 50, 255})

	p := resizer.NewCPUProcessor()
	opts := processor.Options{
		TargetWidth:    200,
		TargetHeight:   200,
		PreserveAspect: true,
		Algorithm:      processor.AlgorithmLanczos,
		JPEGQuality:    85,
	}

	res, err := p.ResizeFile(context.Background(), src, opts)
	if err != nil {
		t.Fatalf("ResizeFile: %v", err)
	}
	if res.OutputPath == "" {
		t.Fatal("OutputPath is empty")
	}

	// Load output and verify dimensions.
	f, _ := os.Open(res.OutputPath)
	defer f.Close()
	got, _, _ := image.Decode(f)
	w, h := got.Bounds().Dx(), got.Bounds().Dy()
	// With 400×300 → fit in 200×200 preserving aspect: 200×150
	if w != 200 || h != 150 {
		t.Errorf("expected 200×150, got %d×%d", w, h)
	}
}

func TestCPUProcessor_ResizeFile_Bilinear(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "input.jpg")
	makeJPEG(t, src, 100, 100, color.RGBA{0, 0, 0, 255})

	p := resizer.NewCPUProcessor()
	opts := processor.Options{
		TargetWidth:    50,
		TargetHeight:   50,
		PreserveAspect: false,
		Algorithm:      processor.AlgorithmBilinear,
		JPEGQuality:    80,
	}

	res, err := p.ResizeFile(context.Background(), src, opts)
	if err != nil {
		t.Fatalf("ResizeFile bilinear: %v", err)
	}

	f, _ := os.Open(res.OutputPath)
	defer f.Close()
	got, _, _ := image.Decode(f)
	w, h := got.Bounds().Dx(), got.Bounds().Dy()
	if w != 50 || h != 50 {
		t.Errorf("expected 50×50, got %d×%d", w, h)
	}
}

func TestCPUProcessor_ResizeFile_PNG(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "input.png")
	makePNG(t, src, 200, 200)

	p := resizer.NewCPUProcessor()
	opts := processor.DefaultOptions()

	res, err := p.ResizeFile(context.Background(), src, opts)
	if err != nil {
		t.Fatalf("ResizeFile PNG: %v", err)
	}
	if filepath.Ext(res.OutputPath) != ".jpg" {
		t.Errorf("output extension should be .jpg, got %q", filepath.Ext(res.OutputPath))
	}
}

func TestCPUProcessor_ResizeFile_CustomOutputDir(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "custom_out")
	src := filepath.Join(dir, "input.jpg")
	makeJPEG(t, src, 100, 100, color.RGBA{})

	p := resizer.NewCPUProcessor()
	opts := processor.DefaultOptions()
	opts.OutputDir = outDir

	res, err := p.ResizeFile(context.Background(), src, opts)
	if err != nil {
		t.Fatalf("ResizeFile custom dir: %v", err)
	}
	if filepath.Dir(res.OutputPath) != outDir {
		t.Errorf("expected output in %q, got %q", outDir, filepath.Dir(res.OutputPath))
	}
}

func TestCPUProcessor_ResizeFile_DefaultResizedSubdir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "photo.jpg")
	makeJPEG(t, src, 100, 100, color.RGBA{})

	p := resizer.NewCPUProcessor()
	opts := processor.DefaultOptions()
	opts.OutputDir = ""

	res, err := p.ResizeFile(context.Background(), src, opts)
	if err != nil {
		t.Fatalf("ResizeFile: %v", err)
	}
	want := filepath.Join(dir, "resized", "photo.jpg")
	if res.OutputPath != want {
		t.Errorf("expected %q, got %q", want, res.OutputPath)
	}
}

func TestCPUProcessor_ResizeDir(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.jpg", "b.jpg"} {
		makeJPEG(t, filepath.Join(dir, name), 100, 100, color.RGBA{100, 100, 100, 255})
	}
	makePNG(t, filepath.Join(dir, "c.png"), 100, 100)

	p := resizer.NewCPUProcessor()
	opts := processor.DefaultOptions()

	ch, err := p.ResizeDir(context.Background(), dir, opts)
	if err != nil {
		t.Fatalf("ResizeDir: %v", err)
	}

	var results []processor.Progress
	for prog := range ch {
		if prog.Result != nil {
			results = append(results, prog)
		}
	}

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Result.Err != nil {
			t.Errorf("file %q failed: %v", r.Result.InputPath, r.Result.Err)
		}
	}
}

func TestCPUProcessor_ResizeDir_Cancellation(t *testing.T) {
	dir := t.TempDir()
	// Create enough files that cancellation is meaningful.
	for i := 0; i < 10; i++ {
		makeJPEG(t, filepath.Join(dir, fmt.Sprintf("img%02d.jpg", i)), 200, 200, color.RGBA{})
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := resizer.NewCPUProcessor()
	opts := processor.DefaultOptions()

	ch, err := p.ResizeDir(ctx, dir, opts)
	if err != nil {
		t.Fatalf("ResizeDir: %v", err)
	}

	cancel() // cancel immediately

	// Drain without hanging
	for range ch {
	}
}

func TestCPUProcessor_ResizeFile_NonexistentInput(t *testing.T) {
	p := resizer.NewCPUProcessor()
	_, err := p.ResizeFile(context.Background(), "/no/such/file.jpg", processor.DefaultOptions())
	if err == nil {
		t.Error("expected error for nonexistent input")
	}
}

// ---- aspect ratio / dimension calculation -----------------------------------

func TestTargetDimensions_PreserveAspect(t *testing.T) {
	cases := []struct {
		srcW, srcH, tw, th int
		wantW, wantH       int
	}{
		// Landscape fits in square box
		{400, 300, 200, 200, 200, 150},
		// Portrait fits in landscape box
		{100, 200, 300, 150, 75, 150},
		// Already smaller than target – no upscale
		{100, 100, 200, 200, 100, 100},
		// Only width constrained
		{800, 400, 400, 0, 400, 200},
		// Only height constrained
		{800, 400, 0, 100, 200, 100},
		// Square → square box
		{300, 300, 100, 100, 100, 100},
	}

	dir := t.TempDir()
	p := resizer.NewCPUProcessor()

	for _, tc := range cases {
		src := filepath.Join(dir, t.Name()+".jpg")
		makeJPEG(t, src, tc.srcW, tc.srcH, color.RGBA{200, 200, 200, 255})

		opts := processor.Options{
			TargetWidth:    tc.tw,
			TargetHeight:   tc.th,
			PreserveAspect: true,
			Algorithm:      processor.AlgorithmBilinear,
			JPEGQuality:    80,
		}

		res, err := p.ResizeFile(context.Background(), src, opts)
		if err != nil {
			t.Errorf("%dx%d→%dx%d: ResizeFile: %v", tc.srcW, tc.srcH, tc.tw, tc.th, err)
			continue
		}

		f, _ := os.Open(res.OutputPath)
		img, _, _ := image.Decode(f)
		f.Close()
		w, h := img.Bounds().Dx(), img.Bounds().Dy()
		if w != tc.wantW || h != tc.wantH {
			t.Errorf("%dx%d fit in %dx%d → expected %dx%d, got %dx%d",
				tc.srcW, tc.srcH, tc.tw, tc.th, tc.wantW, tc.wantH, w, h)
		}

		// cleanup for reuse of same path
		_ = os.Remove(res.OutputPath)
	}
}

// ---- GPUProcessor -----------------------------------------------------------

func TestGPUProcessor_Available(t *testing.T) {
	p, err := resizer.NewGPUProcessor()
	if err != nil {
		t.Fatalf("NewGPUProcessor: %v", err)
	}
	defer p.Close()

	if !p.Available() {
		t.Error("GPUProcessor must always be available (falls back to CPU)")
	}
}

func TestGPUProcessor_ResizeFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "input.jpg")
	makeJPEG(t, src, 300, 200, color.RGBA{255, 128, 0, 255})

	p, err := resizer.NewGPUProcessor()
	if err != nil {
		t.Fatalf("NewGPUProcessor: %v", err)
	}
	defer p.Close()

	opts := processor.Options{
		TargetWidth:    150,
		TargetHeight:   150,
		PreserveAspect: true,
		Algorithm:      processor.AlgorithmLanczos,
		JPEGQuality:    90,
	}

	res, err := p.ResizeFile(context.Background(), src, opts)
	if err != nil {
		t.Fatalf("GPUProcessor.ResizeFile: %v", err)
	}
	if res.OutputPath == "" {
		t.Fatal("OutputPath empty")
	}
	if res.Err != nil {
		t.Fatalf("Result.Err: %v", res.Err)
	}

	f, _ := os.Open(res.OutputPath)
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	// 300×200 fit in 150×150 → 150×100
	if w != 150 || h != 100 {
		t.Errorf("expected 150×100, got %d×%d", w, h)
	}
}

// TestGPUProcessor_Close verifies that calling Close twice does not panic.
func TestGPUProcessor_Close(t *testing.T) {
	p, _ := resizer.NewGPUProcessor()
	_ = p.Close()
	_ = p.Close() // must not panic
}

// TestGPUProcessor_ConcurrentResize verifies that multiple goroutines can call
// ResizeFile simultaneously without data races or errors. This exercises the
// per-call command-queue + cloneKernel path that removed the global mutex.
func TestGPUProcessor_ConcurrentResize(t *testing.T) {
	p, err := resizer.NewGPUProcessor()
	if err != nil {
		t.Fatalf("NewGPUProcessor: %v", err)
	}
	defer p.Close()

	dir := t.TempDir()
	const N = 16
	srcs := make([]string, N)
	for i := 0; i < N; i++ {
		path := filepath.Join(dir, fmt.Sprintf("input%02d.jpg", i))
		makeJPEG(t, path, 400, 300, color.RGBA{uint8(i * 16), 100, 200, 255})
		srcs[i] = path
	}

	opts := processor.Options{
		TargetWidth:    200,
		TargetHeight:   150,
		PreserveAspect: false,
		Algorithm:      processor.AlgorithmLanczos,
		JPEGQuality:    85,
	}

	var wg sync.WaitGroup
	errs := make(chan error, N)

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(src string) {
			defer wg.Done()
			res, err := p.ResizeFile(context.Background(), src, opts)
			if err != nil {
				errs <- fmt.Errorf("ResizeFile(%q): %w", src, err)
				return
			}
			if res.Err != nil {
				errs <- fmt.Errorf("result error for %q: %w", src, res.Err)
				return
			}
			// Verify output dimensions.
			f, err := os.Open(res.OutputPath)
			if err != nil {
				errs <- fmt.Errorf("open output %q: %w", res.OutputPath, err)
				return
			}
			defer f.Close()
			img, _, err := image.Decode(f)
			if err != nil {
				errs <- fmt.Errorf("decode output %q: %w", res.OutputPath, err)
				return
			}
			w, h := img.Bounds().Dx(), img.Bounds().Dy()
			if w != 200 || h != 150 {
				errs <- fmt.Errorf("expected 200×150, got %d×%d for %q", w, h, src)
			}
		}(srcs[i])
	}

	wg.Wait()
	close(errs)

	for e := range errs {
		t.Error(e)
	}
}

// ---- DefaultOptions ---------------------------------------------------------

func TestDefaultOptions(t *testing.T) {
	opts := processor.DefaultOptions()
	if opts.TargetWidth <= 0 {
		t.Error("DefaultOptions.TargetWidth should be positive")
	}
	if opts.TargetHeight <= 0 {
		t.Error("DefaultOptions.TargetHeight should be positive")
	}
	if !opts.PreserveAspect {
		t.Error("DefaultOptions.PreserveAspect should be true")
	}
	if opts.JPEGQuality < 1 || opts.JPEGQuality > 100 {
		t.Errorf("DefaultOptions.JPEGQuality out of range: %d", opts.JPEGQuality)
	}
	if opts.Algorithm == "" {
		t.Error("DefaultOptions.Algorithm should not be empty")
	}
}

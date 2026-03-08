// Package processor defines the image resizing interface and shared types.
// The interface is intentionally minimal so the UI layer only depends on this
// package and not on any GPU or CPU implementation details.
package processor

import "context"

// Algorithm is the interpolation method used when resizing.
type Algorithm string

const (
	// AlgorithmBilinear is fast and produces decent quality for mild downscales.
	AlgorithmBilinear Algorithm = "bilinear"
	// AlgorithmLanczos produces high-quality results, especially for large
	// downscale ratios. Slower than bilinear.
	AlgorithmLanczos Algorithm = "lanczos"
)

// Options controls how images are resized.
type Options struct {
	// TargetWidth is the maximum output width in pixels. 0 means unconstrained.
	TargetWidth int
	// TargetHeight is the maximum output height in pixels. 0 means unconstrained.
	TargetHeight int
	// PreserveAspect keeps the original aspect ratio when both dimensions are
	// set. The output will fit within TargetWidth × TargetHeight.
	PreserveAspect bool
	// Algorithm selects the interpolation kernel. Defaults to Lanczos.
	Algorithm Algorithm
	// JPEGQuality is the output JPEG quality [1-100]. Defaults to 90.
	JPEGQuality int
	// OutputDir overrides the output directory. When empty the processor uses
	// the "resized" sub-directory next to the source file.
	OutputDir string
	// MaxConcurrency limits how many files are processed simultaneously.
	// 0 means use a sensible default (typically number of CPU cores).
	MaxConcurrency int
}

// DefaultOptions returns a ready-to-use Options with all fields populated with
// reasonable defaults.
func DefaultOptions() Options {
	return Options{
		TargetWidth:    1920,
		TargetHeight:   1080,
		PreserveAspect: true,
		Algorithm:      AlgorithmLanczos,
		JPEGQuality:    90,
		MaxConcurrency: 0, // auto
	}
}

// FileResult holds the outcome of processing a single file.
type FileResult struct {
	// InputPath is the absolute path of the source file.
	InputPath string
	// OutputPath is the absolute path of the written output file. Empty on
	// error.
	OutputPath string
	// Err is non-nil when processing this file failed.
	Err error
}

// Progress is sent on the progress channel during a batch run.
type Progress struct {
	// Done is the number of files that have been fully processed so far.
	Done int
	// Total is the total number of files in the batch.
	Total int
	// Current is the path of the file being processed right now.
	Current string
	// Result is non-nil after a file finishes (success or error).
	Result *FileResult
}

// Processor resizes images. Implementations may use GPU or CPU resources.
// The interface is designed for easy mocking in tests.
type Processor interface {
	// Name returns a human-readable identifier for the implementation.
	Name() string

	// Available reports whether this processor can be used on the current
	// machine (e.g. whether an OpenCL platform is present).
	Available() bool

	// ResizeFile resizes a single image file and writes the result.
	ResizeFile(ctx context.Context, inputPath string, opts Options) (FileResult, error)

	// ResizeDir resizes all supported images in inputDir, reporting progress via
	// the returned channel. The channel is closed when all files are done.
	ResizeDir(ctx context.Context, inputDir string, opts Options) (<-chan Progress, error)

	// Close releases any GPU or OS resources held by the processor.
	Close() error
}

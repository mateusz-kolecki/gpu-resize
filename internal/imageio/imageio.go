// Package imageio handles reading and writing of JPEG, PNG and BMP images.
// All output is written as JPEG.
package imageio

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	// Register BMP decoder via side-effect import.
	_ "golang.org/x/image/bmp"
)

// SupportedExtensions lists the file extensions (lowercase, with dot) that
// this package can decode.
var SupportedExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".bmp":  true,
}

// IsSupportedFile returns true when the file at path has a supported image
// extension.
func IsSupportedFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return SupportedExtensions[ext]
}

// Decode reads an image from disk. It supports JPEG, PNG and BMP formats.
func Decode(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("imageio: open %q: %w", path, err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("imageio: decode %q: %w", path, err)
	}
	return img, nil
}

// EncodeJPEG writes img to path as a JPEG with the given quality [1-100].
// Parent directories are created as needed.
func EncodeJPEG(path string, img image.Image, quality int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("imageio: mkdir %q: %w", filepath.Dir(path), err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("imageio: create %q: %w", path, err)
	}
	defer f.Close()

	opts := &jpeg.Options{Quality: clampQuality(quality)}
	if err := jpeg.Encode(f, img, opts); err != nil {
		return fmt.Errorf("imageio: encode %q: %w", path, err)
	}
	return nil
}

// EncodePNG writes img to path as a lossless PNG.
// Parent directories are created as needed.
func EncodePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("imageio: mkdir %q: %w", filepath.Dir(path), err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("imageio: create %q: %w", path, err)
	}
	defer f.Close()

	if err := png.Encode(f, img); err != nil {
		return fmt.Errorf("imageio: encode PNG %q: %w", path, err)
	}
	return nil
}

// OutputPath derives the output file path for a given input path.
// The output is always a .jpg file placed in a "resized" subdirectory of the
// input file's parent, unless outputDir is non-empty.
func OutputPath(inputPath, outputDir string) string {
	base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	outName := base + ".jpg"

	if outputDir != "" {
		return filepath.Join(outputDir, outName)
	}

	dir := filepath.Dir(inputPath)
	return filepath.Join(dir, "resized", outName)
}

// ScanDir returns all supported image paths found directly inside dir.
// It does not recurse into sub-directories.
func ScanDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("imageio: read dir %q: %w", dir, err)
	}

	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if IsSupportedFile(p) {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

func clampQuality(q int) int {
	if q < 1 {
		return 1
	}
	if q > 100 {
		return 100
	}
	return q
}

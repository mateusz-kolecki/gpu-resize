package imageio_test

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/mateusz/gpu-resize/internal/imageio"
	// BMP encoder for creating test fixtures
	_ "golang.org/x/image/bmp"
)

// makeTestImage creates a simple solid-colour RGBA image.
func makeTestImage(w, h int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestIsSupportedFile(t *testing.T) {
	cases := []struct {
		path    string
		want    bool
	}{
		{"photo.jpg", true},
		{"photo.JPG", true},
		{"photo.jpeg", true},
		{"photo.JPEG", true},
		{"photo.png", true},
		{"photo.PNG", true},
		{"photo.bmp", true},
		{"photo.BMP", true},
		{"photo.gif", false},
		{"photo.tiff", false},
		{"photo.webp", false},
		{"noextension", false},
	}

	for _, tc := range cases {
		got := imageio.IsSupportedFile(tc.path)
		if got != tc.want {
			t.Errorf("IsSupportedFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestDecodeJPEG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jpg")

	img := makeTestImage(64, 64, color.RGBA{255, 0, 0, 255})
	f, _ := os.Create(path)
	_ = jpeg.Encode(f, img, &jpeg.Options{Quality: 80})
	f.Close()

	got, err := imageio.Decode(path)
	if err != nil {
		t.Fatalf("Decode JPEG: %v", err)
	}
	if got.Bounds().Dx() != 64 || got.Bounds().Dy() != 64 {
		t.Errorf("unexpected bounds: %v", got.Bounds())
	}
}

func TestDecodePNG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")

	img := makeTestImage(32, 48, color.RGBA{0, 255, 0, 255})
	f, _ := os.Create(path)
	_ = png.Encode(f, img)
	f.Close()

	got, err := imageio.Decode(path)
	if err != nil {
		t.Fatalf("Decode PNG: %v", err)
	}
	if got.Bounds().Dx() != 32 || got.Bounds().Dy() != 48 {
		t.Errorf("unexpected bounds: %v", got.Bounds())
	}
}

func TestDecodeNonexistent(t *testing.T) {
	_, err := imageio.Decode("/nonexistent/path/image.jpg")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestEncodeJPEG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "out.jpg")

	img := makeTestImage(16, 16, color.RGBA{0, 0, 255, 255})
	if err := imageio.EncodeJPEG(path, img, 85); err != nil {
		t.Fatalf("EncodeJPEG: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("output not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("output file is empty")
	}
}

func TestEncodeJPEGCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c", "out.jpg")
	img := makeTestImage(8, 8, color.RGBA{128, 128, 128, 255})

	if err := imageio.EncodeJPEG(nested, img, 75); err != nil {
		t.Fatalf("EncodeJPEG with nested dirs: %v", err)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Fatalf("output not found: %v", err)
	}
}

func TestOutputPath_DefaultResizedSubdir(t *testing.T) {
	got := imageio.OutputPath("/home/user/photos/image.png", "")
	want := "/home/user/photos/resized/image.jpg"
	if got != want {
		t.Errorf("OutputPath = %q, want %q", got, want)
	}
}

func TestOutputPath_CustomDir(t *testing.T) {
	got := imageio.OutputPath("/home/user/photos/vacation.bmp", "/tmp/out")
	want := "/tmp/out/vacation.jpg"
	if got != want {
		t.Errorf("OutputPath = %q, want %q", got, want)
	}
}

func TestOutputPath_ExtensionReplaced(t *testing.T) {
	cases := []struct{ in, out string }{
		{"/a/b/foo.jpeg", "/a/b/resized/foo.jpg"},
		{"/a/b/foo.jpg", "/a/b/resized/foo.jpg"},
		{"/a/b/foo.bmp", "/a/b/resized/foo.jpg"},
		{"/a/b/foo.png", "/a/b/resized/foo.jpg"},
	}
	for _, tc := range cases {
		got := imageio.OutputPath(tc.in, "")
		if got != tc.out {
			t.Errorf("OutputPath(%q) = %q, want %q", tc.in, got, tc.out)
		}
	}
}

func TestScanDir(t *testing.T) {
	dir := t.TempDir()

	// Create supported and unsupported files
	for _, name := range []string{"a.jpg", "b.png", "c.bmp", "d.jpeg"} {
		f, _ := os.Create(filepath.Join(dir, name))
		img := makeTestImage(4, 4, color.RGBA{})
		_ = png.Encode(f, img)
		f.Close()
	}
	// Unsupported files
	for _, name := range []string{"readme.txt", "data.gif"} {
		_ = os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}

	paths, err := imageio.ScanDir(dir)
	if err != nil {
		t.Fatalf("ScanDir: %v", err)
	}
	if len(paths) != 4 {
		t.Errorf("expected 4 paths, got %d: %v", len(paths), paths)
	}
}

func TestScanDir_SkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()
	_ = os.Mkdir(filepath.Join(dir, "subdir"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "subdir", "img.jpg"), []byte{}, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "top.jpg"), []byte{}, 0o644)

	// top.jpg will fail to decode but ScanDir only enumerates.
	paths, err := imageio.ScanDir(dir)
	if err != nil {
		t.Fatalf("ScanDir: %v", err)
	}
	for _, p := range paths {
		if filepath.Base(filepath.Dir(p)) == "subdir" {
			t.Errorf("ScanDir returned file from subdirectory: %s", p)
		}
	}
}

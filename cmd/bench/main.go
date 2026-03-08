//go:build ignore

package main

import (
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"runtime/pprof"
	"time"

	"github.com/mateusz/gpu-resize/internal/processor"
	"github.com/mateusz/gpu-resize/pkg/resizer"
)

func makeJPEG(path string, w, h int) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix { img.Pix[i] = uint8(i) }
	f, _ := os.Create(path)
	defer f.Close()
	jpeg.Encode(f, img, &jpeg.Options{Quality: 90})
}

func main() {
	dir, _ := os.MkdirTemp("", "bench")
	defer os.RemoveAll(dir)

	fmt.Println("Creating test images...")
	for i := 0; i < 20; i++ {
		makeJPEG(filepath.Join(dir, fmt.Sprintf("img%03d.jpg", i)), 4000, 3000)
	}

	p, _ := resizer.NewGPUProcessor()
	defer p.Close()
	fmt.Println("Processor:", p.Name())

	opts := processor.DefaultOptions()

	f, _ := os.Create("/tmp/cpu.pprof")
	pprof.StartCPUProfile(f)

	start := time.Now()
	ch, _ := p.ResizeDir(context.Background(), dir, opts)
	for range ch {}
	elapsed := time.Since(start)

	pprof.StopCPUProfile()
	f.Close()

	fmt.Printf("Done in %v\n", elapsed)
}

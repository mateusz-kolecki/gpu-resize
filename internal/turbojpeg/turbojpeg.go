// Package turbojpeg wraps the libjpeg-turbo TurboJPEG API for fast SIMD-
// accelerated JPEG decode and encode.
//
// DecodeRGBA decompresses a JPEG file directly to a flat RGBA byte slice,
// avoiding the Go stdlib YCbCr intermediate and all per-pixel conversion.
//
// EncodeRGBA compresses a flat RGBA byte slice to a JPEG file using the
// libjpeg-turbo DCT/Huffman implementation, which is ~3-5× faster than the
// pure-Go encoder.
package turbojpeg

/*
#cgo linux   LDFLAGS: -lturbojpeg
#cgo darwin  LDFLAGS: -lturbojpeg
#cgo windows LDFLAGS: -lturbojpeg

#include <turbojpeg.h>
#include <stdlib.h>
#include <string.h>
*/
import "C"
import (
	"fmt"
	"os"
	"unsafe"
)

// DecodeRGBA reads a JPEG file from disk and returns the raw RGBA pixels,
// width, and height. The returned slice is owned by the caller and backed by
// a Go allocation (safe for GC).
func DecodeRGBA(path string) (pix []byte, w, h int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("turbojpeg: read %q: %w", path, err)
	}
	return DecodeRGBABytes(data)
}

// DecodeRGBABytes decompresses in-memory JPEG bytes to raw RGBA.
func DecodeRGBABytes(data []byte) (pix []byte, w, h int, err error) {
	handle := C.tjInitDecompress()
	if handle == nil {
		return nil, 0, 0, fmt.Errorf("turbojpeg: tjInitDecompress failed")
	}
	defer C.tjDestroy(handle)

	var cw, ch, subsamp, colorspace C.int
	if C.tjDecompressHeader3(handle,
		(*C.uchar)(unsafe.Pointer(&data[0])), C.ulong(len(data)),
		&cw, &ch, &subsamp, &colorspace) != 0 {
		return nil, 0, 0, fmt.Errorf("turbojpeg: tjDecompressHeader3: %s", C.GoString(C.tjGetErrorStr()))
	}

	w, h = int(cw), int(ch)
	pix = make([]byte, w*h*4)

	if C.tjDecompress2(handle,
		(*C.uchar)(unsafe.Pointer(&data[0])), C.ulong(len(data)),
		(*C.uchar)(unsafe.Pointer(&pix[0])),
		cw, 0 /*pitch*/, ch,
		C.TJPF_RGBA, C.TJFLAG_FASTDCT) != 0 {
		return nil, 0, 0, fmt.Errorf("turbojpeg: tjDecompress2: %s", C.GoString(C.tjGetErrorStr()))
	}

	return pix, w, h, nil
}

// EncodeRGBA compresses raw RGBA pixels to a JPEG file on disk.
// quality is [1, 100]. Parent directories are created as needed.
func EncodeRGBA(path string, pix []byte, w, h, quality int) error {
	handle := C.tjInitCompress()
	if handle == nil {
		return fmt.Errorf("turbojpeg: tjInitCompress failed")
	}
	defer C.tjDestroy(handle)

	var outBuf *C.uchar
	var outSize C.ulong

	if C.tjCompress2(handle,
		(*C.uchar)(unsafe.Pointer(&pix[0])),
		C.int(w), 0 /*pitch*/, C.int(h),
		C.TJPF_RGBA,
		&outBuf, &outSize,
		C.TJSAMP_420,
		C.int(quality),
		C.TJFLAG_FASTDCT) != 0 {
		return fmt.Errorf("turbojpeg: tjCompress2: %s", C.GoString(C.tjGetErrorStr()))
	}
	defer C.tjFree(outBuf)

	// Copy from C-managed buffer to Go slice before freeing.
	jpegBytes := C.GoBytes(unsafe.Pointer(outBuf), C.int(outSize))

	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return fmt.Errorf("turbojpeg: mkdir: %w", err)
	}
	if err := os.WriteFile(path, jpegBytes, 0o644); err != nil {
		return fmt.Errorf("turbojpeg: write %q: %w", path, err)
	}
	return nil
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}

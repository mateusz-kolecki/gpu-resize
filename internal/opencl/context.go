// Package opencl wraps the OpenCL C API for image resizing.
// Build tags ensure this file is compiled only when OpenCL is available.
// On platforms without OpenCL the CPU fallback is used instead.
package opencl

/*
#cgo linux   LDFLAGS: -lOpenCL
#cgo darwin  LDFLAGS: -framework OpenCL
#cgo windows LDFLAGS: -lOpenCL

// Target OpenCL 2.0 – broad support across all GPU vendors and integrated
// graphics. Suppresses the CL_TARGET_OPENCL_VERSION pragma note and opts out
// of the deprecated clCreateCommandQueue warning (use the 2.0 variant).
#define CL_TARGET_OPENCL_VERSION 210

#ifdef __APPLE__
  #include <OpenCL/opencl.h>
#else
  #include <CL/cl.h>
#endif

#include <stdlib.h>
#include <string.h>

// clGetPlatformIDs wrapper for easier error handling.
cl_int getPlatforms(cl_uint *num, cl_platform_id *platforms, cl_uint max) {
    return clGetPlatformIDs(max, platforms, num);
}
*/
import "C"
import (
	"errors"
	"fmt"
	"image"
	"sync"
	"unsafe"
)

// rgbaPool recycles large RGBA scratch buffers to avoid hammering the GC.
// Each entry is a []byte sized for the largest image seen so far; smaller
// images simply use a prefix of the slice.
var rgbaPool = sync.Pool{
	New: func() any { return new([]byte) },
}

// ErrNoOpenCL is returned when no OpenCL platform is found on the system.
var ErrNoOpenCL = errors.New("opencl: no platform found")

// Device represents an OpenCL device (GPU or CPU).
type Device struct {
	platform C.cl_platform_id
	device   C.cl_device_id
	name     string
	isGPU    bool
}

// Name returns the OpenCL device name.
func (d *Device) Name() string { return d.name }

// IsGPU returns true when the device is a GPU (discrete or integrated).
func (d *Device) IsGPU() bool { return d.isGPU }

// Context wraps an OpenCL context + compiled program + kernels.
// It is safe for concurrent use: each resize call creates its own private
// command queue so that multiple goroutines can drive the GPU in parallel
// without any mutex serialisation.
type Context struct {
	device  Device
	ctx     C.cl_context
	program C.cl_program

	// Kernel objects are immutable after build and may be read concurrently.
	// OpenCL kernels are NOT thread-safe for clSetKernelArg; we clone a
	// kernel per job instead of sharing them.
	kernelBilinearSrc     C.cl_kernel
	kernelLanczosHorizSrc C.cl_kernel
	kernelLanczosVertSrc  C.cl_kernel

	closeOnce sync.Once
}

// PickBestDevice selects the most capable OpenCL device available.
// Preference order: discrete GPU > integrated GPU > CPU device.
// Returns ErrNoOpenCL when no platform/device is found.
func PickBestDevice() (Device, error) {
	var numPlatforms C.cl_uint
	var platforms [16]C.cl_platform_id

	ret := C.getPlatforms(&numPlatforms, &platforms[0], 16)
	if ret != C.CL_SUCCESS || numPlatforms == 0 {
		return Device{}, ErrNoOpenCL
	}

	var best Device
	found := false

	for i := 0; i < int(numPlatforms); i++ {
		plat := platforms[i]

		// Try GPU first, then CPU as fallback.
		for _, devType := range []C.cl_device_type{C.CL_DEVICE_TYPE_GPU, C.CL_DEVICE_TYPE_CPU} {
			var numDevices C.cl_uint
			var devs [8]C.cl_device_id
			if C.clGetDeviceIDs(plat, devType, 8, &devs[0], &numDevices) != C.CL_SUCCESS {
				continue
			}
			if numDevices == 0 {
				continue
			}

			dev := devs[0]
			name := deviceInfoString(dev, C.CL_DEVICE_NAME)
			isGPU := devType == C.CL_DEVICE_TYPE_GPU

			// Always prefer a GPU; only fall back to CPU if no GPU found yet.
			if !found || (isGPU && !best.isGPU) {
				best = Device{
					platform: plat,
					device:   dev,
					name:     name,
					isGPU:    isGPU,
				}
				found = true
				if isGPU {
					// GPU found – no need to check CPU devices on this platform.
					break
				}
			}
		}
	}

	if !found {
		return Device{}, ErrNoOpenCL
	}
	return best, nil
}

// NewContext compiles the resize kernels on dev and returns a ready-to-use
// Context. Call Close when done.
func NewContext(dev Device) (*Context, error) {
	var errCode C.cl_int

	ctx := C.clCreateContext(nil, 1, &dev.device, nil, nil, &errCode)
	if errCode != C.CL_SUCCESS {
		return nil, fmt.Errorf("opencl: clCreateContext: %d", errCode)
	}

	source := KernelBilinear + "\n" + KernelLanczos
	cSource := C.CString(source)
	defer C.free(unsafe.Pointer(cSource))
	length := C.size_t(len(source))

	prog := C.clCreateProgramWithSource(ctx, 1, &cSource, &length, &errCode)
	if errCode != C.CL_SUCCESS {
		C.clReleaseContext(ctx)
		return nil, fmt.Errorf("opencl: clCreateProgramWithSource: %d", errCode)
	}

	buildRet := C.clBuildProgram(prog, 1, &dev.device, nil, nil, nil)
	if buildRet != C.CL_SUCCESS {
		log := buildLog(prog, dev.device)
		C.clReleaseProgram(prog)
		C.clReleaseContext(ctx)
		return nil, fmt.Errorf("opencl: clBuildProgram: %s", log)
	}

	mkKernel := func(name string) (C.cl_kernel, error) {
		cname := C.CString(name)
		defer C.free(unsafe.Pointer(cname))
		k := C.clCreateKernel(prog, cname, &errCode)
		if errCode != C.CL_SUCCESS {
			return nil, fmt.Errorf("opencl: clCreateKernel(%s): %d", name, errCode)
		}
		return k, nil
	}

	kBilinear, err := mkKernel("resize_bilinear")
	if err != nil {
		C.clReleaseProgram(prog)
		C.clReleaseContext(ctx)
		return nil, err
	}

	kLanH, err := mkKernel("lanczos_horizontal")
	if err != nil {
		C.clReleaseKernel(kBilinear)
		C.clReleaseProgram(prog)
		C.clReleaseContext(ctx)
		return nil, err
	}

	kLanV, err := mkKernel("lanczos_vertical")
	if err != nil {
		C.clReleaseKernel(kBilinear)
		C.clReleaseKernel(kLanH)
		C.clReleaseProgram(prog)
		C.clReleaseContext(ctx)
		return nil, err
	}

	return &Context{
		device:                dev,
		ctx:                   ctx,
		program:               prog,
		kernelBilinearSrc:     kBilinear,
		kernelLanczosHorizSrc: kLanH,
		kernelLanczosVertSrc:  kLanV,
	}, nil
}

// DeviceName returns the name of the underlying OpenCL device.
func (c *Context) DeviceName() string { return c.device.name }

// IsGPU returns whether the underlying device is a GPU.
func (c *Context) IsGPU() bool { return c.device.isGPU }

// ResizeBilinear resizes src to dstW×dstH using bilinear interpolation on GPU.
// It is safe to call from multiple goroutines simultaneously.
func (c *Context) ResizeBilinear(src image.Image, dstW, dstH int) (image.Image, error) {
	rgba, srcW, srcH, fromPool := toRGBA(src)
	if fromPool {
		defer putRGBA(&rgba)
	}
	srcSize := C.size_t(srcW * srcH * 4)
	dstSize := C.size_t(dstW * dstH * 4)

	// Each call gets its own command queue → true parallel GPU dispatch.
	queue, err := c.newQueue()
	if err != nil {
		return nil, err
	}
	defer C.clReleaseCommandQueue(queue)

	// Clone the kernel so clSetKernelArg is not shared with other goroutines.
	k, err := c.cloneKernel(c.kernelBilinearSrc)
	if err != nil {
		return nil, err
	}
	defer C.clReleaseKernel(k)

	var errCode C.cl_int

	srcBuf := C.clCreateBuffer(c.ctx, C.CL_MEM_READ_ONLY|C.CL_MEM_COPY_HOST_PTR,
		srcSize, unsafe.Pointer(&rgba[0]), &errCode)
	if errCode != C.CL_SUCCESS {
		return nil, fmt.Errorf("opencl: clCreateBuffer src: %d", errCode)
	}
	defer C.clReleaseMemObject(srcBuf)

	dstBuf := C.clCreateBuffer(c.ctx, C.CL_MEM_WRITE_ONLY, dstSize, nil, &errCode)
	if errCode != C.CL_SUCCESS {
		return nil, fmt.Errorf("opencl: clCreateBuffer dst: %d", errCode)
	}
	defer C.clReleaseMemObject(dstBuf)

	C.clSetKernelArg(k, 0, C.size_t(unsafe.Sizeof(srcBuf)), unsafe.Pointer(&srcBuf))
	C.clSetKernelArg(k, 1, C.size_t(unsafe.Sizeof(dstBuf)), unsafe.Pointer(&dstBuf))
	setIntArg(k, 2, srcW)
	setIntArg(k, 3, srcH)
	setIntArg(k, 4, dstW)
	setIntArg(k, 5, dstH)

	gws := [2]C.size_t{C.size_t(roundUp(dstW, 16)), C.size_t(roundUp(dstH, 16))}
	lws := [2]C.size_t{16, 16}
	if C.clEnqueueNDRangeKernel(queue, k, 2, nil, &gws[0], &lws[0], 0, nil, nil) != C.CL_SUCCESS {
		return nil, fmt.Errorf("opencl: bilinear enqueue failed")
	}

	return readResult(queue, dstBuf, dstSize, dstW, dstH)
}

// ResizeLanczos resizes src to dstW×dstH using a Lanczos-3 filter on GPU.
// Two kernel passes are used (separable filter).
// It is safe to call from multiple goroutines simultaneously.
func (c *Context) ResizeLanczos(src image.Image, dstW, dstH int) (image.Image, error) {
	rgba, srcW, srcH, fromPool := toRGBA(src)
	if fromPool {
		defer putRGBA(&rgba)
	}
	srcSize := C.size_t(srcW * srcH * 4)
	// Intermediate: srcH rows × dstW cols, float4 (16 bytes per pixel)
	tmpSize := C.size_t(srcH * dstW * 16)
	dstSize := C.size_t(dstW * dstH * 4)

	queue, err := c.newQueue()
	if err != nil {
		return nil, err
	}
	defer C.clReleaseCommandQueue(queue)

	kH, err := c.cloneKernel(c.kernelLanczosHorizSrc)
	if err != nil {
		return nil, err
	}
	defer C.clReleaseKernel(kH)

	kV, err := c.cloneKernel(c.kernelLanczosVertSrc)
	if err != nil {
		C.clReleaseKernel(kH)
		return nil, err
	}
	defer C.clReleaseKernel(kV)

	var errCode C.cl_int

	srcBuf := C.clCreateBuffer(c.ctx, C.CL_MEM_READ_ONLY|C.CL_MEM_COPY_HOST_PTR,
		srcSize, unsafe.Pointer(&rgba[0]), &errCode)
	if errCode != C.CL_SUCCESS {
		return nil, fmt.Errorf("opencl: clCreateBuffer src: %d", errCode)
	}
	defer C.clReleaseMemObject(srcBuf)

	tmpBuf := C.clCreateBuffer(c.ctx, C.CL_MEM_READ_WRITE, tmpSize, nil, &errCode)
	if errCode != C.CL_SUCCESS {
		return nil, fmt.Errorf("opencl: clCreateBuffer tmp: %d", errCode)
	}
	defer C.clReleaseMemObject(tmpBuf)

	dstBuf := C.clCreateBuffer(c.ctx, C.CL_MEM_WRITE_ONLY, dstSize, nil, &errCode)
	if errCode != C.CL_SUCCESS {
		return nil, fmt.Errorf("opencl: clCreateBuffer dst: %d", errCode)
	}
	defer C.clReleaseMemObject(dstBuf)

	// --- Horizontal pass ---
	C.clSetKernelArg(kH, 0, C.size_t(unsafe.Sizeof(srcBuf)), unsafe.Pointer(&srcBuf))
	C.clSetKernelArg(kH, 1, C.size_t(unsafe.Sizeof(tmpBuf)), unsafe.Pointer(&tmpBuf))
	setIntArg(kH, 2, srcW)
	setIntArg(kH, 3, srcH)
	setIntArg(kH, 4, dstW)

	gwsH := [2]C.size_t{C.size_t(roundUp(dstW, 16)), C.size_t(roundUp(srcH, 16))}
	lws := [2]C.size_t{16, 16}
	if C.clEnqueueNDRangeKernel(queue, kH, 2, nil, &gwsH[0], &lws[0], 0, nil, nil) != C.CL_SUCCESS {
		return nil, fmt.Errorf("opencl: lanczos horizontal enqueue failed")
	}

	// --- Vertical pass ---
	C.clSetKernelArg(kV, 0, C.size_t(unsafe.Sizeof(tmpBuf)), unsafe.Pointer(&tmpBuf))
	C.clSetKernelArg(kV, 1, C.size_t(unsafe.Sizeof(dstBuf)), unsafe.Pointer(&dstBuf))
	setIntArg(kV, 2, srcH)
	setIntArg(kV, 3, dstW)
	setIntArg(kV, 4, dstH)

	gwsV := [2]C.size_t{C.size_t(roundUp(dstW, 16)), C.size_t(roundUp(dstH, 16))}
	if C.clEnqueueNDRangeKernel(queue, kV, 2, nil, &gwsV[0], &lws[0], 0, nil, nil) != C.CL_SUCCESS {
		return nil, fmt.Errorf("opencl: lanczos vertical enqueue failed")
	}

	return readResult(queue, dstBuf, dstSize, dstW, dstH)
}

// Close releases all OpenCL resources. Safe to call multiple times.
func (c *Context) Close() {
	c.closeOnce.Do(func() {
		C.clReleaseKernel(c.kernelBilinearSrc)
		C.clReleaseKernel(c.kernelLanczosHorizSrc)
		C.clReleaseKernel(c.kernelLanczosVertSrc)
		C.clReleaseProgram(c.program)
		C.clReleaseContext(c.ctx)
	})
}

// ---- helpers ----------------------------------------------------------------

// newQueue creates a fresh in-order command queue for one resize job.
// Command queues are cheap to create and allow true parallel dispatch.
func (c *Context) newQueue() (C.cl_command_queue, error) {
	var errCode C.cl_int
	q := C.clCreateCommandQueueWithProperties(c.ctx, c.device.device, nil, &errCode)
	if errCode != C.CL_SUCCESS {
		return nil, fmt.Errorf("opencl: clCreateCommandQueueWithProperties: %d", errCode)
	}
	return q, nil
}

// cloneKernel creates an independent copy of a kernel so that concurrent
// callers can each call clSetKernelArg without data races.
func (c *Context) cloneKernel(src C.cl_kernel) (C.cl_kernel, error) {
	var errCode C.cl_int
	k := C.clCloneKernel(src, &errCode)
	if errCode != C.CL_SUCCESS {
		return nil, fmt.Errorf("opencl: clCloneKernel: %d", errCode)
	}
	return k, nil
}

// toRGBA converts any image.Image to a flat RGBA byte slice suitable for
// upload to the GPU. Returns (pixels, width, height).
//
// Fast paths (no per-pixel virtual dispatch):
//   - *image.RGBA   – zero-copy, returns Pix directly
//   - *image.YCbCr  – direct YCbCr→RGB math (JPEG decoder output)
//   - *image.NRGBA  – direct pre-multiplied alpha conversion (PNG decoder output)
//
// The slow generic path is retained as a fallback for exotic types.
// For the pooled paths a buffer is taken from rgbaPool and returned via
// putRGBA; callers that own the buffer should call putRGBA when done.
// The zero-copy *image.RGBA path never allocates.
func toRGBA(src image.Image) (pix []byte, w, h int, fromPool bool) {
	b := src.Bounds()
	w, h = b.Dx(), b.Dy()

	// ---- Fast path 1: already *image.RGBA ----
	if rgba, ok := src.(*image.RGBA); ok && b.Min.X == 0 && b.Min.Y == 0 {
		return rgba.Pix, w, h, false
	}

	// Acquire a scratch buffer from the pool.
	need := w * h * 4
	ptr := rgbaPool.Get().(*[]byte)
	if cap(*ptr) < need {
		*ptr = make([]byte, need)
	}
	out := (*ptr)[:need]

	// ---- Fast path 2: *image.YCbCr (standard JPEG decoder output) ----
	if ycc, ok := src.(*image.YCbCr); ok {
		convertYCbCr(out, ycc, w, h)
		return out, w, h, true
	}

	// ---- Fast path 3: *image.NRGBA (standard PNG decoder output) ----
	if nrgba, ok := src.(*image.NRGBA); ok {
		convertNRGBA(out, nrgba, b, w, h)
		return out, w, h, true
	}

	// ---- Slow generic fallback ----
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := (y*w + x) * 4
			out[i+0] = uint8(r >> 8)
			out[i+1] = uint8(g >> 8)
			out[i+2] = uint8(bl >> 8)
			out[i+3] = uint8(a >> 8)
		}
	}
	return out, w, h, true
}

// putRGBA returns a buffer obtained from toRGBA back to the pool.
// Must only be called when fromPool was true.
func putRGBA(ptr *[]byte) {
	rgbaPool.Put(ptr)
}

// convertYCbCr converts an *image.YCbCr directly to interleaved RGBA bytes.
// This is significantly faster than going through the color.RGBA interface
// because it avoids per-pixel heap allocations and virtual dispatch.
// Works for all chroma subsampling ratios (4:4:4, 4:2:2, 4:2:0, etc.).
func convertYCbCr(out []byte, ycc *image.YCbCr, w, h int) {
	rx, ry := ycc.Rect.Min.X, ycc.Rect.Min.Y
	for y := 0; y < h; y++ {
		ay := ry + y
		rowBase := y * w * 4
		for x := 0; x < w; x++ {
			ax := rx + x
			yi := ycc.YOffset(ax, ay)
			ci := ycc.COffset(ax, ay)

			// Inline the ITU-R BT.601 YCbCr→RGB conversion used by color.YCbCr.RGBA().
			yy := int32(ycc.Y[yi]) * 0x10101
			cb := int32(ycc.Cb[ci]) - 128
			cr := int32(ycc.Cr[ci]) - 128

			r := (yy + 91881*cr + 1<<15) >> 16
			g := (yy - 22554*cb - 46802*cr + 1<<15) >> 16
			b := (yy + 116130*cb + 1<<15) >> 16

			if r < 0 {
				r = 0
			} else if r > 255 {
				r = 255
			}
			if g < 0 {
				g = 0
			} else if g > 255 {
				g = 255
			}
			if b < 0 {
				b = 0
			} else if b > 255 {
				b = 255
			}

			i := rowBase + x*4
			out[i+0] = uint8(r)
			out[i+1] = uint8(g)
			out[i+2] = uint8(b)
			out[i+3] = 0xff
		}
	}
}

// convertNRGBA converts an *image.NRGBA to interleaved RGBA.
// PNG images with full opacity are handled with a fast copy; semi-transparent
// pixels apply pre-multiplied alpha.
func convertNRGBA(out []byte, src *image.NRGBA, b image.Rectangle, w, h int) {
	for y := 0; y < h; y++ {
		srcRow := src.Pix[src.PixOffset(b.Min.X, b.Min.Y+y):]
		dstRow := out[y*w*4:]
		for x := 0; x < w; x++ {
			si := x * 4
			a := srcRow[si+3]
			if a == 0xff {
				// Fully opaque: copy directly.
				dstRow[si+0] = srcRow[si+0]
				dstRow[si+1] = srcRow[si+1]
				dstRow[si+2] = srcRow[si+2]
				dstRow[si+3] = 0xff
			} else if a == 0 {
				dstRow[si+0] = 0
				dstRow[si+1] = 0
				dstRow[si+2] = 0
				dstRow[si+3] = 0
			} else {
				// Pre-multiply alpha for the GPU kernel (which works in linear RGBA).
				fa := uint32(a)
				dstRow[si+0] = uint8(uint32(srcRow[si+0]) * fa / 255)
				dstRow[si+1] = uint8(uint32(srcRow[si+1]) * fa / 255)
				dstRow[si+2] = uint8(uint32(srcRow[si+2]) * fa / 255)
				dstRow[si+3] = a
			}
		}
	}
}

// readResult reads the output buffer from the GPU and returns an *image.RGBA.
// Uses a blocking read then a single copy into the image's Pix slice.
func readResult(queue C.cl_command_queue, buf C.cl_mem, size C.size_t, w, h int) (image.Image, error) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if C.clEnqueueReadBuffer(queue, buf, C.CL_TRUE, 0, size,
		unsafe.Pointer(&img.Pix[0]), 0, nil, nil) != C.CL_SUCCESS {
		return nil, fmt.Errorf("opencl: clEnqueueReadBuffer failed")
	}
	return img, nil
}

func setIntArg(k C.cl_kernel, idx int, val int) {
	v := C.cl_int(val)
	C.clSetKernelArg(k, C.cl_uint(idx), C.size_t(unsafe.Sizeof(v)), unsafe.Pointer(&v))
}

func roundUp(n, multiple int) int {
	return ((n + multiple - 1) / multiple) * multiple
}

func deviceInfoString(dev C.cl_device_id, param C.cl_device_info) string {
	var size C.size_t
	C.clGetDeviceInfo(dev, param, 0, nil, &size)
	buf := make([]C.char, size)
	C.clGetDeviceInfo(dev, param, size, unsafe.Pointer(&buf[0]), nil)
	return C.GoString(&buf[0])
}

func buildLog(prog C.cl_program, dev C.cl_device_id) string {
	var size C.size_t
	C.clGetProgramBuildInfo(prog, dev, C.CL_PROGRAM_BUILD_LOG, 0, nil, &size)
	if size == 0 {
		return "(no log)"
	}
	buf := make([]C.char, size)
	C.clGetProgramBuildInfo(prog, dev, C.CL_PROGRAM_BUILD_LOG, size, unsafe.Pointer(&buf[0]), nil)
	return C.GoString(&buf[0])
}

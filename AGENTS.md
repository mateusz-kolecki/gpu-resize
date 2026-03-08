# Agent Context: gpu-resize

This file gives an AI coding agent everything it needs to contribute to this
repository without prior conversation history.

---

## What this project is

A desktop application that batch-resizes photos using the GPU. Stack:

- **Go 1.22** backend — image I/O, OpenCL, libjpeg-turbo, business logic
- **Wails v2** — embeds the Go binary + Svelte UI into a single native window
- **Svelte 4** frontend — thin UI layer, communicates via Wails bindings
- **OpenCL** (via cgo) — GPU kernels for bilinear and Lanczos-3 resize
- **libjpeg-turbo** (via cgo) — SIMD JPEG decode/encode, ~3–5× faster than stdlib
- **GitHub Actions** — builds `gpu-resize.exe` on Windows using MSYS2/MinGW-w64

The UI is in Polish (photographer audience).

---

## Repository layout

```
.
├── main.go                         # wails.Run entry point
├── app.go                          # Wails-bound API (SelectDirectory, ResizeDir, …)
├── wails.json                      # outputfilename: gpu-resize
├── go.mod                          # module github.com/mateusz/gpu-resize, go 1.22
│
├── internal/
│   ├── processor/processor.go      # Processor interface + Options / Progress / FileResult
│   ├── imageio/imageio.go          # Decode (JPEG/PNG/BMP), EncodeJPEG, ScanDir, OutputPath
│   ├── opencl/
│   │   ├── context.go              # cgo: OpenCL context, command queues, toRGBA, kernels
│   │   └── kernels.go              # OpenCL C source strings (bilinear + Lanczos)
│   └── turbojpeg/turbojpeg.go      # cgo: DecodeRGBA / EncodeRGBA via libjpeg-turbo
│
├── pkg/resizer/
│   ├── resizer.go                  # GPUProcessor, CPUProcessor, resizeDir
│   └── resizer_test.go
│
├── frontend/src/App.svelte         # Complete UI in one file
│
├── cmd/bench/main.go               # Benchmark + pprof (go:build ignore)
│
└── .github/workflows/
    └── build-windows.yml           # CI: MSYS2 → Windows .exe
```

---

## Key interfaces and types

### `internal/processor/processor.go`

```go
type Processor interface {
    Name() string
    Available() bool
    ResizeFile(ctx context.Context, inputPath string, opts Options) (FileResult, error)
    ResizeDir(ctx context.Context, inputDir string, opts Options) (<-chan Progress, error)
    Close() error
}

type Options struct {
    TargetWidth, TargetHeight int
    PreserveAspect            bool
    Algorithm                 Algorithm   // "bilinear" | "lanczos"
    JPEGQuality               int         // 1–100
    OutputDir                 string      // "" = <input>/resized/
    MaxConcurrency            int         // 0 = auto (NumCPU*4)
}

type Progress struct {
    Done, Total int
    Current     string
    Result      *FileResult   // non-nil when a file finishes
}
```

### `app.go` — Wails-exposed methods

| Method | Notes |
|---|---|
| `ProcessorInfo() string` | Returns GPU device name or "CPU (no OpenCL)" |
| `DefaultOptions() Options` | 1920×1080, Lanczos, quality 90 |
| `SelectDirectory() string` | Opens native OS folder picker |
| `ResizeDir(dir string, opts Options) error` | Non-blocking; emits `resize:progress` events |
| `CancelResize()` | Cancels context of the running batch |

The event payload is `ProgressPayload{Done, Total, Current, Error, Finished}`.

---

## Build instructions

### Linux (dev machine — NVIDIA GTX 1070, webkit2gtk-4.1)

```bash
# Install deps once
sudo apt install ocl-icd-opencl-dev opencl-headers libturbojpeg0-dev

# Dev mode
wails dev -tags webkit2_41

# Production build
wails build -tags webkit2_41
```

### Tests

```bash
go test -race ./internal/... ./pkg/...
```

Tests never require a GPU — `NewGPUProcessor` falls back to CPU silently.

### Benchmark

```bash
# Generates 20 synthetic 4000×3000 JPEGs in /tmp/bench-images/ and resizes them
# Writes CPU profile to /tmp/cpu.pprof
go run cmd/bench/main.go
go tool pprof -http=: /tmp/cpu.pprof
```

---

## Design decisions and constraints

### Why OpenCL (not CUDA / Vulkan / Metal)?

OpenCL runs on every modern GPU — NVIDIA, AMD, Intel integrated, Apple Silicon.
A single code path covers all platforms. CUDA would lock out non-NVIDIA users;
Vulkan Compute would require a much larger driver/API surface.

### Why `clCloneKernel` + per-call command queues?

`cl_kernel` objects are NOT thread-safe for `clSetKernelArg`. The original code
used a global mutex, which serialised all GPU work. Replacing it with
`clCloneKernel` (OpenCL 2.1) gives each goroutine its own kernel copy, and
each goroutine also creates its own `cl_command_queue`. The GPU driver
multiplexes the queues. This halved wall time on the GTX 1070.

`CL_TARGET_OPENCL_VERSION 210` is required in the cgo preamble.

### Why libjpeg-turbo?

After fixing the GPU concurrency bottleneck, `pprof` showed JPEG decode (Go
stdlib `image/jpeg`, ~40%) and encode (~13%) as the dominant costs. The Go
JPEG decoder outputs `*image.YCbCr`; converting to RGBA for the GPU kernel
consumed another 35%. libjpeg-turbo decodes directly to RGBA (no intermediate)
and uses SIMD for the DCT. This cut the benchmark from ~2.8 s to **1.44 s**.

### Why `sync.Pool` for RGBA scratch buffers?

After turbojpeg, the Go stdlib path (PNG/BMP) still allocates a large `[]byte`
per image. Pool reuse eliminates GC pressure in long batches. The pool holds
`*[]byte` (pointer to slice) so the pool can grow the slice without allocating
a new pool entry.

### Why `NumCPU × 4` concurrency?

Decode and encode happen on CPU goroutines while the GPU kernel runs
asynchronously. Overlapping decode of the next image with GPU execution of the
current one keeps the GPU busy. 4× was determined empirically on the GTX 1070;
lower values left the GPU idle, higher values added contention without benefit.

### Output format: JPEG only

Simplifies the encoder path. JPEG at quality ≥ 85 is indistinguishable from
source for typical photography. The UI exposes three quality presets:
Web (75), High (85), Max (95). PNG output would require a different encoder
(libjpeg-turbo handles JPEG only) and is not a priority.

### No upscaling

`targetDimensions` caps the scale factor at 1.0. Upscaling introduces
artefacts and is rarely needed in a photographer's workflow.

---

## Known issues / current state

### Windows CI build

The GitHub Actions workflow (`.github/workflows/build-windows.yml`) uses
`msys2/setup-msys2` + `pacman` to install MinGW-w64 toolchain, OpenCL headers,
and libjpeg-turbo. Previous attempts to use the Khronos SDK zip failed because
the zip ships MSVC `.lib` files which MinGW's `ld` cannot link against.

**Status as of last known run:** the MSYS2-based workflow has been committed
but not yet confirmed green. Check:
```bash
gh run list --repo mateusz-kolecki/gpu-resize --limit 5
```

### libjpeg-turbo on macOS

`turbojpeg.go` uses `#cgo darwin LDFLAGS: -lturbojpeg`. If `brew install
jpeg-turbo` installs to a non-standard prefix (e.g. on Apple Silicon:
`/opt/homebrew/`) the build will fail unless `CGO_CFLAGS` and `CGO_LDFLAGS`
point there. Has not been tested on macOS.

---

## What to work on next

Possible directions (not ordered):

1. **Confirm Windows CI green** — watch the current run, fix any remaining
   linker issues. The MSYS2 approach should be correct but hasn't been
   verified yet.

2. **macOS support** — test the build on Apple Silicon; fix the Homebrew path
   for libjpeg-turbo.

3. **Output format options** — add PNG output for lossless results.
   `CPUProcessor` can use `image/png`; `GPUProcessor` would need a PNG
   encoder (libspng via cgo, or Go stdlib as a fallback).

4. **OpenCL image objects** — replace `cl_mem` linear buffers with
   `CL_MEM_OBJECT_IMAGE2D`. GPU texture caches are optimised for 2-D access
   patterns and can improve kernel throughput significantly.

5. **Progress refinement** — the `resize:progress` event currently fires once
   before and once after each file. Adding per-file timing and a per-file
   throughput estimate would improve the UI.

6. **Automated benchmarks in CI** — add a `go test -bench` target that
   generates synthetic images and asserts throughput doesn't regress.

---

## Environment notes (dev machine)

- OS: Linux (Debian/Ubuntu-based)
- GPU: NVIDIA GeForce GTX 1070, OpenCL 2.1 (`clCloneKernel` confirmed working)
- `webkit2gtk-4.1` installed → always build with `-tags webkit2_41`
- Git remote: `https://github.com/mateusz-kolecki/gpu-resize.git`
- Authentication: `gh auth setup-git` (HTTPS via GitHub CLI token)
- Branch: `main`

# Architecture

## Package layout

```
gpu-resize/
├── main.go                         # Wails entry point — wails.Run, asset embed
├── app.go                          # App struct: Wails-bound API + event bridge
│
├── internal/
│   ├── processor/
│   │   └── processor.go            # Processor interface + Options / Progress types
│   ├── imageio/
│   │   └── imageio.go              # Decode (JPEG/PNG/BMP), EncodeJPEG, ScanDir, OutputPath
│   ├── opencl/
│   │   ├── context.go              # OpenCL context, command queues, kernel cloning, toRGBA
│   │   └── kernels.go              # OpenCL C kernel source strings
│   └── turbojpeg/
│       └── turbojpeg.go            # cgo wrapper: DecodeRGBA / EncodeRGBA via libjpeg-turbo
│
├── pkg/resizer/
│   ├── resizer.go                  # GPUProcessor, CPUProcessor, resizeDir, targetDimensions
│   └── resizer_test.go
│
├── frontend/src/
│   └── App.svelte                  # Svelte UI (photographer-focused, dark/light theme)
│
├── cmd/bench/main.go               # Standalone benchmark tool (go:build ignore)
└── .github/workflows/
    └── build-windows.yml           # CI: MSYS2 + MinGW-w64 → Windows .exe artifact
```

---

## Dependency graph

```
App (app.go)
  └── processor.Processor (interface)
        └── resizer.GPUProcessor  ──► opencl.Context  ──► OpenCL ICD (GPU driver)
              │                   ──► turbojpeg        ──► libjpeg-turbo (SIMD)
              │                   ──► imageio          ──► Go stdlib (PNG / BMP)
              └── resizer.CPUProcessor (fallback)
                    └── imageio + golang.org/x/image
```

The `App` struct in `app.go` holds only a `processor.Processor` interface value.
It never imports `opencl`, `turbojpeg`, or any GPU code directly. This keeps
the Wails layer thin and makes it straightforward to add a CLI entry point or
swap the backend.

---

## Data flow for a single JPEG file (GPU path)

```
1. ResizeFile called
2. turbojpeg.DecodeRGBA  → []byte (flat RGBA, no YCbCr intermediate)
3. opencl.Context.ResizeLanczos / ResizeBilinear
     a. toRGBA()         → fast path: *image.RGBA.Pix used as-is (zero copy)
     b. clCreateBuffer   → copy RGBA bytes to GPU VRAM
     c. clEnqueueNDRangeKernel (horizontal Lanczos pass)
     d. clEnqueueNDRangeKernel (vertical Lanczos pass)
     e. clEnqueueReadBuffer   → copy result back to *image.RGBA
4. turbojpeg.EncodeRGBA  → write JPEG file via SIMD encoder
```

For PNG/BMP the decode uses Go stdlib (`imageio.Decode`), which returns
`*image.NRGBA` or `*image.RGBA`. The `toRGBA` fast path handles both without
per-pixel virtual dispatch.

---

## Concurrency model

`resizeDir` launches `runtime.NumCPU() × 4` goroutines simultaneously (a
semaphore channel controls the cap). Each goroutine calls `ResizeFile`
independently. Inside the GPU processor, every call:

- Creates its own `cl_command_queue` (`clCreateCommandQueueWithProperties`)
- Clones the template kernels (`clCloneKernel`, requires OpenCL 2.1)

This means there is **no global mutex** anywhere in the hot path. The OpenCL
driver (and GPU hardware scheduler) decides how to multiplex the queues.

Benchmark on NVIDIA GTX 1070 (20× 4000×3000 JPEG):

| Stage | Time |
|---|---|
| Initial (single queue, global mutex) | 5.64 s |
| After per-call queue + clCloneKernel | 3.45 s |
| After sync.Pool + YCbCr fast paths   | ~2.8 s |
| After libjpeg-turbo decode/encode    | **1.44 s** |

After libjpeg-turbo, `pprof` shows 97% of CPU time in `runtime.cgocall` — all
Go-side overhead has been eliminated. Remaining time is SIMD JPEG decode/encode
and OpenCL API calls.

---

## OpenCL kernel design

### Bilinear (`resize_bilinear`)

- 2-D NDRange: one work-item per output pixel
- Local work size: 16×16
- Reads four neighbouring source pixels, linearly interpolates in X then Y
- Single kernel pass (no intermediate buffer)

### Lanczos-3 (`lanczos_horizontal` + `lanczos_vertical`)

- Separable filter: two kernel passes share one intermediate `float4` buffer
  (`srcH × dstW` pixels, 16 bytes each)
- Horizontal pass: for each output column, sum the Lanczos-weighted source
  pixels across a ±3 tap window; store as `float4` to preserve precision
- Vertical pass: accumulate the intermediate rows and write final `uchar4`
  output
- `sinc(x) * sinc(x/3)` computed with `native_sin` / `native_divide` for speed

`clCloneKernel` (OpenCL 2.1) is used to give each concurrent call its own
kernel object so `clSetKernelArg` is race-free.

---

## libjpeg-turbo integration

`internal/turbojpeg/turbojpeg.go` is a minimal cgo wrapper around the
TurboJPEG API:

| Function | What it does |
|---|---|
| `DecodeRGBA(path)` | reads file → `tjDecompress2` with `TJPF_RGBA` → `[]byte` |
| `DecodeRGBABytes(data)` | same but from in-memory bytes |
| `EncodeRGBA(path, pix, w, h, quality)` | `tjCompress2` with `TJSAMP_420` → writes file |

The RGBA pixel layout matches what the OpenCL kernels expect, so there is no
conversion step between decode and GPU upload.

`CPUProcessor` continues to use `imageio.Decode` + `imageio.EncodeJPEG` (Go
stdlib), which is simpler and has no native dependency.

---

## GPU / CPU fallback strategy

`resizer.NewGPUProcessor` always succeeds:

1. `opencl.PickBestDevice` scans all platforms, preferring discrete GPU >
   integrated GPU > OpenCL CPU device.
2. If no platform is found, or `clBuildProgram` fails, `GPUProcessor.clCtx` is
   `nil` and every `ResizeFile` call delegates to an embedded `CPUProcessor`.
3. `GPUProcessor.Name()` returns `"CPU (no OpenCL)"` in fallback mode so the
   UI shows the correct label.

This means callers never need to handle the "no GPU" case explicitly.

---

## UI layer (`frontend/src/App.svelte`)

The Svelte frontend communicates with the Go backend through Wails-generated
TypeScript bindings (in `frontend/src/lib/wailsjs/`). The bindings are
generated automatically by `wails build` / `wails dev`.

**Exposed Go methods (via `app.go`):**

| Method | Frontend usage |
|---|---|
| `ProcessorInfo() string` | Shows GPU/CPU pill in header |
| `DefaultOptions() Options` | Populates initial form state |
| `SelectDirectory() string` | Native folder picker |
| `ResizeDir(dir, opts)` | Starts batch; returns immediately |
| `CancelResize()` | Aborts in-flight batch |

**Event bus:** `ResizeDir` emits `resize:progress` events on the Wails event
bus (not via the return value) so the UI updates in real time without polling.

**Theme:** The UI uses CSS custom properties that switch automatically via
`@media (prefers-color-scheme: dark)`. No JS theme toggle is needed.

---

## Build system

| Command | Purpose |
|---|---|
| `wails dev` | Dev server with hot-reload |
| `wails build` | Production binary with embedded frontend |
| `wails build -tags webkit2_41` | Required on Linux with `webkit2gtk-4.1` |
| `go test -race ./...` | Backend tests with race detector |
| `go run cmd/bench/main.go` | CPU profiler benchmark (writes `/tmp/cpu.pprof`) |

The CI workflow (`.github/workflows/build-windows.yml`) uses the
`msys2/setup-msys2` action to install a complete MinGW-w64 toolchain including
`mingw-w64-x86_64-opencl-icd`, `mingw-w64-x86_64-opencl-headers`, and
`mingw-w64-x86_64-libjpeg-turbo` via `pacman`. This avoids the need to
download and install vendor SDKs manually.

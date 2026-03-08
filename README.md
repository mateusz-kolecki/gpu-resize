# GPU Image Resizer

A desktop application for batch-resizing images using GPU acceleration via
OpenCL.  It handles JPEG, PNG, and BMP input and always outputs JPEG.

Built with [Wails v2](https://wails.io) (Go + Svelte), it works on
**Windows, Linux, and macOS** including machines with integrated GPUs.

---

## Architecture

```
gpu-resize/
├── cmd/gpu-resize/        # Wails entry point + App bindings
│   ├── main.go            # wails.Run, asset embed
│   └── app.go             # App struct (Processor + Wails event bridge)
│
├── internal/
│   ├── processor/         # Processor interface + shared types (no GPU code)
│   │   └── processor.go
│   ├── imageio/           # Decode (JPEG/PNG/BMP) + JPEG encode + path helpers
│   │   └── imageio.go
│   └── opencl/            # OpenCL C kernels (embedded) + cgo context wrapper
│       ├── kernels.go
│       └── context.go
│
├── pkg/resizer/           # GPUProcessor + CPUProcessor (implement Processor)
│   └── resizer.go
│
└── frontend/              # Svelte UI
    └── src/App.svelte
```

**Key design rule:** the UI (`cmd/gpu-resize/app.go`) depends only on
`internal/processor.Processor`. To replace the Wails GUI with a CLI, web API,
or another GUI toolkit you only need to provide a new consumer of that
interface – no GPU or image I/O code changes required.

---

## GPU backend: OpenCL

OpenCL was chosen because it runs on every modern GPU:

| Platform | OpenCL support |
|---|---|
| NVIDIA (discrete) | via CUDA toolkit / OpenCL ICD |
| AMD (discrete) | ROCm or Adrenalin drivers |
| Intel integrated (HD/Iris/Arc) | Intel OpenCL runtime (Windows/Linux/macOS) |
| Apple Silicon / Intel Mac | built-in via `OpenCL.framework` |
| CPU fallback | pocl (Portable OpenCL) or Intel CPU runtime |

If no OpenCL platform is found the app falls back to a pure-Go CPU
implementation transparently – the user still gets correct output, just
without GPU acceleration. The active processor name is shown in the UI header.

---

## Prerequisites

### All platforms
- Go 1.21+
- [Wails v2 CLI](https://wails.io/docs/gettingstarted/installation): `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- Node.js 18+ (for the frontend build)

### Linux
```bash
# Debian / Ubuntu
sudo apt install ocl-icd-opencl-dev opencl-headers

# For Intel integrated GPU
sudo apt install intel-opencl-icd

# For NVIDIA
# Install CUDA toolkit (includes OpenCL headers and ICD)
```

### macOS
OpenCL is included via `OpenCL.framework` – no extra install needed.

### Windows
- Install your GPU vendor's driver (NVIDIA/AMD/Intel) – they all ship OpenCL.
- Install [TDM-GCC](https://jmeubank.github.io/tdm-gcc/) or MSYS2 for cgo.

---

## Building

```bash
# Development (hot-reload)
wails dev

# Production build (native binary + embedded UI)
wails build

# Output: build/bin/gpu-resize (Linux/macOS) or build/bin/gpu-resize.exe
```

---

## Running tests

The tests cover:
- `internal/imageio` – file I/O, path helpers, directory scanning
- `internal/processor` – interface contract via mock, option validation
- `pkg/resizer` – CPU and GPU processors, aspect ratio, all supported formats

```bash
# Run all backend tests (no OpenCL required – GPU tests fall back to CPU)
go test ./internal/... ./pkg/...

# Verbose
go test -v ./internal/... ./pkg/...

# Race detector
go test -race ./internal/... ./pkg/...
```

> The GPU processor tests pass even without an OpenCL-capable GPU because
> `NewGPUProcessor` gracefully falls back to the CPU implementation.

---

## Usage

1. Launch the app.
2. Click **Browse…** and select a folder containing images.
3. Adjust **Max Width / Height** (default 1920×1080) and toggle
   **Preserve aspect ratio** (default: on).
4. Choose the algorithm:
   - **Lanczos** (default) – highest quality, best for large downscales.
   - **Bilinear** – faster, good for mild rescales.
5. Click **Resize All Images**.

Output files are written to a `resized/` sub-directory next to the source
images, with the same base name and a `.jpg` extension. You can override the
output directory in the input field.

---

## Extending / replacing the UI

The `processor.Processor` interface (`internal/processor/processor.go`) is the
only contract the UI layer needs:

```go
type Processor interface {
    Name() string
    Available() bool
    ResizeFile(ctx context.Context, inputPath string, opts Options) (FileResult, error)
    ResizeDir(ctx context.Context, inputDir string, opts Options) (<-chan Progress, error)
    Close() error
}
```

A CLI front-end example:

```go
p, _ := resizer.NewGPUProcessor()
defer p.Close()

ch, _ := p.ResizeDir(context.Background(), "/my/photos", processor.DefaultOptions())
for prog := range ch {
    if prog.Result != nil {
        fmt.Printf("[%d/%d] %s\n", prog.Done, prog.Total, prog.Result.OutputPath)
    }
}
```

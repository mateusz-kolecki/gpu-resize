# GPU Image Resizer

A desktop application for batch-resizing images using GPU acceleration via
OpenCL. It handles JPEG, PNG, and BMP input and always outputs JPEG.

Built with [Wails v2](https://wails.io) (Go + Svelte), it works on
**Windows, Linux, and macOS** including machines with integrated GPUs.

---

## Features

- GPU-accelerated resizing via OpenCL (NVIDIA, AMD, Intel, Apple Silicon)
- Automatic CPU fallback when no OpenCL platform is found
- Lanczos-3 and bilinear interpolation kernels running on the GPU
- SIMD JPEG decode/encode via libjpeg-turbo (3–5× faster than Go stdlib)
- Fully concurrent: `4 × NumCPU` files in-flight simultaneously, each with its
  own OpenCL command queue — the GPU is always fed
- Photographer-friendly UI: resolution presets, quality tiers, dark/light theme
- Output goes to a `resized/` sub-directory next to the source images

---

## Prerequisites

### All platforms
- Go 1.22+
- [Wails v2 CLI](https://wails.io/docs/gettingstarted/installation):
  `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- Node.js 18+

### Linux
```bash
# Debian / Ubuntu
sudo apt install ocl-icd-opencl-dev opencl-headers libturbojpeg0-dev

# Intel integrated GPU
sudo apt install intel-opencl-icd

# NVIDIA — install CUDA toolkit (ships OpenCL ICD)
```

Build with the webkit2_41 tag (required when `webkit2gtk-4.1` is installed):
```bash
wails build -tags webkit2_41
```

### macOS
OpenCL is provided by `OpenCL.framework` — no extra install needed.
Install libjpeg-turbo via Homebrew:
```bash
brew install jpeg-turbo
```

### Windows
GPU drivers (NVIDIA/AMD/Intel) ship OpenCL — no extra install needed.
Install build tools and libraries via MSYS2 MINGW64:
```bash
pacman -S mingw-w64-x86_64-gcc \
          mingw-w64-x86_64-opencl-icd \
          mingw-w64-x86_64-opencl-headers \
          mingw-w64-x86_64-libjpeg-turbo
```

---

## Building

```bash
# Development (hot-reload frontend, Go backend rebuilds on save)
wails dev

# Production build — native binary with embedded UI
wails build

# Linux: required if system has webkit2gtk-4.1
wails build -tags webkit2_41

# Output: build/bin/gpu-resize  (Linux/macOS)
#         build/bin/gpu-resize.exe  (Windows)
```

---

## Running tests

```bash
# All backend packages
go test ./internal/... ./pkg/...

# With race detector (GPU concurrency test runs 16 goroutines simultaneously)
go test -race ./internal/... ./pkg/...

# Verbose
go test -v ./internal/... ./pkg/...
```

Tests pass without a GPU — `NewGPUProcessor` transparently falls back to the
CPU implementation when no OpenCL platform is found.

---

## Usage

1. Launch the app.
2. Click the folder zone (or drag a folder onto it) to select a source directory.
3. Choose a resolution preset: **Instagram 1080×1080**, **Full HD**, **2K**, or
   **Custom** (enter your own dimensions).
4. Select a quality tier: **Web** (75), **High** (85), or **Max** (95).
5. Toggle **Preserve aspect ratio** (default: on) and pick the algorithm
   (**Lanczos** for quality, **Bilinear** for speed).
6. Click **Zmień rozmiar** (Resize).

Output files are written to a `resized/` sub-directory next to the source
images, with the same base name and a `.jpg` extension.

---

## CI / Releases

A GitHub Actions workflow (`.github/workflows/build-windows.yml`) builds the
Windows `.exe` on every push to `main` using MSYS2 + MinGW-w64. The artifact
`gpu-resize-windows` is uploaded and available for download from the Actions
tab.

---

## Architecture

See [`docs/architecture.md`](docs/architecture.md) for a full description of
the package layout, data flow, and design decisions.

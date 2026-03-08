# Detect the host OS and set the default build target accordingly.
# Override with: make build-windows / make build-linux / make build-darwin
UNAME := $(shell uname -s 2>/dev/null || echo Windows)

ifeq ($(UNAME),Linux)
  DEFAULT_BUILD := build-linux
else ifeq ($(UNAME),Darwin)
  DEFAULT_BUILD := build-darwin
else
  DEFAULT_BUILD := build-windows
endif

# On Linux, auto-detect which WebKit2GTK version is available.
# Modern distros (Ubuntu 23.04+, Debian 12+) ship 4.1; older ones ship 4.0.
# The webkit2_41 tag tells Wails to link against webkit2gtk-4.1.
ifeq ($(UNAME),Linux)
  WEBKIT_41 := $(shell pkg-config --exists webkit2gtk-4.1 2>/dev/null && echo yes)
  ifeq ($(WEBKIT_41),yes)
    WEBKIT_TAG := -tags webkit2_41
  endif
endif

.PHONY: dev build build-linux build-darwin build-windows \
        test test-race test-verbose clean

# ── Default target ────────────────────────────────────────────────────────────

build: $(DEFAULT_BUILD)

# ── Development ───────────────────────────────────────────────────────────────

dev:
	wails dev $(WEBKIT_TAG)

# ── Platform builds ───────────────────────────────────────────────────────────

build-linux:
	wails build -platform linux/amd64 -o gpu-resize $(WEBKIT_TAG)

build-darwin:
	wails build -platform darwin/universal -o gpu-resize

build-windows:
	wails build -platform windows/amd64 -o gpu-resize.exe

# ── Testing ───────────────────────────────────────────────────────────────────

test:
	go test ./internal/... ./pkg/...

test-race:
	go test -race ./internal/... ./pkg/...

test-verbose:
	go test -v ./internal/... ./pkg/...

# ── Housekeeping ──────────────────────────────────────────────────────────────

clean:
	rm -rf build/bin frontend/dist

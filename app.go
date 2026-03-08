package main

import (
	"context"
	"fmt"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/mateusz/gpu-resize/internal/applog"
	"github.com/mateusz/gpu-resize/internal/processor"
	"github.com/mateusz/gpu-resize/pkg/resizer"
)

// App is the Wails application struct. All exported methods become callable
// from the frontend via the generated TypeScript bindings.
//
// The struct depends only on the processor.Processor interface so the GPU
// implementation can be swapped out (e.g. replaced with a pure-CPU version
// during tests or future CLI usage).
type App struct {
	ctx  context.Context
	proc processor.Processor

	// cancel lets us abort an in-progress batch.
	cancel context.CancelFunc
}

// NewApp constructs the App and selects the best available processor.
func NewApp() *App {
	return &App{}
}

// startup is called by the Wails runtime once the window is ready.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	applog.Printf("startup: initialising processor")
	gpu, err := resizer.NewGPUProcessor()
	if err != nil {
		applog.Errorf("startup: NewGPUProcessor returned error: %v — falling back to CPU", err)
		a.proc = resizer.NewCPUProcessor()
	} else {
		a.proc = gpu
	}
	applog.Printf("startup: processor selected: %s", a.proc.Name())

	// Notify the frontend that the backend is ready. The frontend waits for
	// this event before calling ProcessorInfo() so it never races with the
	// GPU/OpenCL initialisation that happens above.
	wailsruntime.EventsEmit(ctx, "app:ready", a.proc.Name())
}

// shutdown is called by the Wails runtime when the window is closing.
func (a *App) shutdown(_ context.Context) {
	if a.cancel != nil {
		a.cancel()
	}
	_ = a.proc.Close()
}

// ---- Wails-exposed API ------------------------------------------------------

// ProcessorInfo returns a human-readable description of the active processor.
func (a *App) ProcessorInfo() string {
	return a.proc.Name()
}

// DefaultOptions returns the default resize options.
func (a *App) DefaultOptions() processor.Options {
	return processor.DefaultOptions()
}

// SelectDirectory opens a native folder picker and returns the chosen path.
func (a *App) SelectDirectory() (string, error) {
	dir, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Wybierz katalog ze zdjęciami",
	})
	if err != nil {
		return "", err
	}
	return dir, nil
}

// ResizeProgressEvent is the event name emitted on the Wails event bus.
const ResizeProgressEvent = "resize:progress"

// ProgressPayload is the JSON-serialisable progress update sent to the
// frontend via Wails events.
type ProgressPayload struct {
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Current string `json:"current"`
	// Error is non-empty when the current file failed.
	Error string `json:"error,omitempty"`
	// Finished is true when the whole batch is complete.
	Finished bool `json:"finished,omitempty"`
}

// ResizeDir starts resizing all images in inputDir and streams progress events
// to the frontend via the "resize:progress" event. It is non-blocking – the
// frontend listens for events and calls CancelResize if needed.
func (a *App) ResizeDir(inputDir string, opts processor.Options) error {
	// Cancel any running batch.
	if a.cancel != nil {
		a.cancel()
	}

	applog.Printf("ResizeDir: dir=%q opts=%+v", inputDir, opts)

	batchCtx, cancel := context.WithCancel(a.ctx)
	a.cancel = cancel

	ch, err := a.proc.ResizeDir(batchCtx, inputDir, opts)
	if err != nil {
		cancel()
		applog.Errorf("ResizeDir: proc.ResizeDir failed: %v", err)
		return fmt.Errorf("failed to start resize: %w", err)
	}

	go func() {
		defer cancel()
		for prog := range ch {
			payload := ProgressPayload{
				Done:    prog.Done,
				Total:   prog.Total,
				Current: prog.Current,
			}
			if prog.Result != nil && prog.Result.Err != nil {
				payload.Error = prog.Result.Err.Error()
				applog.Errorf("ResizeDir: file error: file=%q err=%v", prog.Current, prog.Result.Err)
			}
			applog.Printf("ResizeDir: progress done=%d total=%d current=%q", prog.Done, prog.Total, prog.Current)
			wailsruntime.EventsEmit(a.ctx, ResizeProgressEvent, payload)
			// Yield for one frame so WebView2 can dispatch and render each
			// progress event before the next one arrives. Without this, the
			// JS microtask queue batches all events into a single tick and
			// Svelte skips straight to the finished state with no animation.
			time.Sleep(16 * time.Millisecond)
		}
		applog.Printf("ResizeDir: batch complete, emitting finished event")
		wailsruntime.EventsEmit(a.ctx, ResizeProgressEvent, ProgressPayload{Finished: true})
	}()

	return nil
}

// CancelResize aborts the current batch if one is running.
func (a *App) CancelResize() {
	if a.cancel != nil {
		a.cancel()
	}
}

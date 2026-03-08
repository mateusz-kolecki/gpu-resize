// Package applog writes timestamped log lines to gpu-resize.log next to the
// running executable.  It is a thin wrapper around the standard log package.
// All functions are safe to call before Init (they become no-ops until the
// file is open).
package applog

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

var logger *log.Logger
var logFile *os.File

// Init opens (or creates) gpu-resize.log in the same directory as the
// executable and configures the package-level logger.  Call once from main
// or startup.  Returns the path of the log file for informational purposes.
func Init() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		exe = "."
	}
	dir := filepath.Dir(exe)
	path := filepath.Join(dir, "gpu-resize.log")

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return path, fmt.Errorf("applog: cannot open log file %s: %w", path, err)
	}
	logFile = f

	// Write to both the file and stderr so it shows up in a terminal too.
	w := io.MultiWriter(f, os.Stderr)
	logger = log.New(w, "", 0)

	Printf("=== gpu-resize started %s ===", time.Now().Format("2006-01-02 15:04:05"))
	return path, nil
}

// Close flushes and closes the log file.
func Close() {
	if logFile != nil {
		_ = logFile.Sync()
		_ = logFile.Close()
		logFile = nil
	}
}

// Printf logs a formatted message with a timestamp prefix.
func Printf(format string, args ...any) {
	if logger == nil {
		return
	}
	logger.Printf("%s  "+format, append([]any{time.Now().Format("15:04:05.000")}, args...)...)
}

// Errorf logs a formatted error message.
func Errorf(format string, args ...any) {
	Printf("ERROR "+format, args...)
}

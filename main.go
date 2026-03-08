// Package main is the Wails application entry point.
package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/mateusz/gpu-resize/internal/applog"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if path, err := applog.Init(); err != nil {
		// Can't open log file – not fatal, just print to stderr.
		println("WARNING: could not open log file:", path, err.Error())
	}
	defer applog.Close()

	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "Zmiana rozmiaru obrazów GPU",
		Width:  860,
		Height: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 18, G: 18, B: 18, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		panic(err)
	}
}

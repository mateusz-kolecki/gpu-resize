//go:build debugui

package main

// debugOpenInspector returns true when the app is built with -tags debugui.
// This causes the WebView2 DevTools panel to open automatically on startup,
// making it easy to inspect the JS console, events and DOM without a separate
// debugger setup.
func debugOpenInspector() bool { return true }

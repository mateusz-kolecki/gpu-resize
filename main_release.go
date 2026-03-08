//go:build !debugui

package main

// debugOpenInspector returns false in normal production builds.
func debugOpenInspector() bool { return false }

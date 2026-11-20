package telemetry

import (
	"strconv"

	"golang.org/x/sys/windows"
)

// osMajor returns "win-10" or "win-11" (Windows 11 still reports major 10, build >= 22000).
func osMajor() string {
	v := windows.RtlGetVersion()
	if v == nil || v.MajorVersion == 0 {
		return "unknown"
	}
	if v.MajorVersion == 10 && v.BuildNumber >= 22000 {
		return "win-11"
	}
	return majorLabel("win", strconv.Itoa(int(v.MajorVersion)))
}

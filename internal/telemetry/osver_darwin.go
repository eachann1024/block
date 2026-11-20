package telemetry

import (
	"strings"
	"syscall"
)

// osMajor returns e.g. "macos-15"; only the major version is sent.
func osMajor() string {
	v, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return "unknown"
	}
	return majorLabel("macos", strings.SplitN(v, ".", 2)[0])
}

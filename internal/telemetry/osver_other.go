//go:build !darwin && !windows

package telemetry

// osMajor reports only the OS family on Linux and others; distro versions vary too much to be useful.
func osMajor() string {
	return "linux"
}

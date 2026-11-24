//go:build !unix

package plugins

// stageOwnerPIDAlive reports whether pid is known to be alive on non-Unix hosts.
// Returning known=false lets the caller fall back to the conservative age TTL.
func stageOwnerPIDAlive(pid int) (alive bool, known bool) {
	return false, false
}

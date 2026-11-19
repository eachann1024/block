//go:build !darwin

package files

func darwinBundleIntact() bool {
	return true
}

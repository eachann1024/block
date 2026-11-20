//go:build !darwin

package main

func maybeRunMacOpenRelay() bool {
	return false
}

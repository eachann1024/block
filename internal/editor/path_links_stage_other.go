//go:build !windows && !darwin

package editor

import "os"

func preserveOriginSecurityMark(_ string, _ *os.File, _ *os.File, _ string) ([]byte, bool, error) {
	return nil, false, nil
}

func verifyOriginSecurityMark(_ *os.File, _ []byte) error {
	return nil
}

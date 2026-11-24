//go:build !windows

package app

import "fmt"

func shellExecute(file, _ string) error {
	return fmt.Errorf("shellExecute is not available for %q", file)
}

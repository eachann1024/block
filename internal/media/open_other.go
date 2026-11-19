//go:build !unix

package media

import "os"

func openLocalFile(root *os.Root, path string) (*os.File, error) {
	return root.Open(path)
}

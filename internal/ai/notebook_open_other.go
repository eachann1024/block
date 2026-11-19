//go:build !unix

package ai

import "os"

func openNoteForRead(root *os.Root, name string) (*os.File, error) {
	return root.Open(name)
}

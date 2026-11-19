//go:build darwin || linux

package ai

import (
	"errors"
	"os"
	"syscall"
)

func privateCredentialPath(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && info.Mode().Perm()&0o077 == 0
}

func lockProviderCredentials(f *os.File) (func(), error) {
	fd := int(f.Fd())
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			return func() { _ = syscall.Flock(fd, syscall.LOCK_UN) }, err
		}
	}
}

func syncCredentialDirectory(dir *os.Root) error {
	f, err := dir.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func protectProviderCredentials(data []byte) ([]byte, error) {
	return data, nil
}

func unprotectProviderCredentials(data []byte) ([]byte, error) {
	return data, nil
}

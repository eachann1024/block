//go:build unix

package files

import "syscall"

const oNonBlockFlag = syscall.O_NONBLOCK

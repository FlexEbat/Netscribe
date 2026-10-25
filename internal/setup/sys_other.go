//go:build !linux

package setup

import (
	"errors"
	"io/fs"
	"syscall"
)

// Netscribe runs on Linux only. These stubs let the package build elsewhere, so that
// "go vet ./..." works on a development machine.
var errNotLinux = errors.New("not supported on this system")

func freeBytes(string) (uint64, error) { return 0, errNotLinux }

func credential(Account) *syscall.SysProcAttr { return nil }

func ownerOf(fs.FileInfo) (uid, gid uint32, ok bool) { return 0, 0, false }

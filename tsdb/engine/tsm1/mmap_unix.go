// +build !windows,!plan9

package tsm1

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func mmap(f *os.File, offset int64, length int) ([]byte, error) {
	// anonymous mapping
	if f == nil {
		return unix.Mmap(-1, 0, length, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	}

	mmap, err := unix.Mmap(int(f.Fd()), 0, length, syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, err
	}

	return mmap, nil
}

func munmap(b []byte) (err error) {
	return unix.Munmap(b)
}

// madviseWillNeed gives the kernel the mmap madvise value MADV_WILLNEED, hinting
// that we plan on using the provided buffer in the near future.
func madviseWillNeed(b []byte) error {
	return madvise(b, syscall.MADV_WILLNEED)
}

func madviseDontNeed(b []byte) error {
	return madvise(b, syscall.MADV_DONTNEED)
}

// From: github.com/boltdb/bolt/bolt_unix.go
func madvise(b []byte, advice int) (err error) {
	return unix.Madvise(b, advice)
}

// Fadvise
func fadviseWillNeed(fd uintptr, l int64) error {
	return fadvise(fd, l, unix.FADV_WILLNEED)
}

func fadviseDontNeed(fd uintptr, l int64) error {
	return fadvise(fd, l, unix.FADV_DONTNEED)
}
func fadvise(fd uintptr, l int64, advice int) (err error) {
	return unix.Fadvise(int(fd), 0, l, advice)
}

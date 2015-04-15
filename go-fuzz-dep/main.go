package gofuzzdep

import (
	"syscall"
	"unsafe"
)

var data [1 << 20]byte

func Main(f func([]byte) int) {
	n, _ := syscall.Read(0, data[:])
	f(data[:n])
}

var (
	CoverTab     *[64 << 10]byte
	fakeCoverTab [64 << 10]byte
)

func init() {
	shm, _ := syscall.Getenv("__AFL_SHM_ID")
	if shm != "" {
		shmID := atoi(shm)
		shmMem, _, errno := syscall.Syscall(syscall.SYS_SHMAT, uintptr(shmID), 0, 0)
		if errno != 0 {
			println("failed to shmat")
			syscall.Exit(1)
		}
		CoverTab = (*[64 << 10]byte)(unsafe.Pointer(shmMem))
	} else {
		CoverTab = &fakeCoverTab
	}
}

func atoi(s string) uintptr {
	var v uintptr
	for _, x := range s {
		v = v*10 + uintptr(x) - '0'
	}
	return v
}
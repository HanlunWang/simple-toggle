//go:build !darwin

package engine

import (
	"syscall"
	"time"
)

func waitExit(pid int) {
	for syscall.Kill(pid, 0) == nil {
		time.Sleep(2 * time.Second)
	}
}

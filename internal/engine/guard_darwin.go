package engine

import (
	"time"

	"golang.org/x/sys/unix"
)

// waitExit 用 kqueue 等进程退出：阻塞在内核里，期间不唤醒、不占 CPU。
func waitExit(pid int) {
	kq, err := unix.Kqueue()
	if err != nil {
		pollExit(pid)
		return
	}
	defer unix.Close(kq)
	var ev unix.Kevent_t
	unix.SetKevent(&ev, pid, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_ONESHOT)
	ev.Fflags = unix.NOTE_EXIT
	out := make([]unix.Kevent_t, 1)
	for {
		n, err := unix.Kevent(kq, []unix.Kevent_t{ev}, out, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil || n > 0 {
			return // ESRCH：进程已经不在了
		}
	}
}

func pollExit(pid int) {
	for unix.Kill(pid, 0) == nil {
		time.Sleep(2 * time.Second)
	}
}

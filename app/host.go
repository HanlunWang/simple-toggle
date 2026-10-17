package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// appHost 用 mygo 实现 automation.Host：全局快捷键、文件监听与通知。
type appHost struct{}

func (appHost) RegisterShortcut(accel string, fn func()) error {
	return mygo.GlobalShortcut.Register(accel, fn)
}

func (appHost) UnregisterShortcut(accel string) { mygo.GlobalShortcut.Unregister(accel) }

func (appHost) Watch(path string, fn func()) (func(), error) {
	path = expandHome(path)
	ctx, cancel := context.WithCancel(context.Background())
	w, err := mygo.WatchFiles(ctx, path, mygo.FileWatchOptions{Recursive: true, Debounce: 500 * time.Millisecond, MaxDelay: 2 * time.Second})
	if err != nil {
		cancel()
		return nil, err
	}
	go func() {
		for {
			evs, err := w.Next(ctx)
			if err != nil {
				return
			}
			if len(evs) > 0 {
				fn()
			}
		}
	}()
	return func() { cancel(); _ = w.Close() }, nil
}

func (appHost) Notify(title, body string) {
	_ = mygo.NewNotification(mygo.NotificationOptions{Title: title, Body: body}).Show()
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

package app

import (
	"context"
	"log"
	"sync"
	"time"

	mygo "github.com/egoist/mygo"
)

// 自动更新：启动 30 秒后检查一次，之后每 6 小时一次。设置里关掉「下载并安装更新」就不再检查
// （这是 app 唯一的对外请求）。找到新版本就下载、校验签名并替换安装位置的 app；
// 正在运行的这份不受影响，下次启动（或在设置里点「立即重启」）就是新版本。

const (
	updateFirstCheck = 30 * time.Second
	updateEvery      = 6 * time.Hour
)

type updates struct {
	mu    sync.Mutex
	busy  bool
	ready string // 已经装好、等待重启的版本
}

func (u *updates) readyVersion() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.ready
}

// startUpdates 在后台定时检查并安装更新；开发版与无法写入安装位置的 app 不检查。
func (c *controller) startUpdates() {
	if !mygo.Updater.Enabled() {
		return
	}
	go func() {
		t := time.NewTimer(updateFirstCheck)
		defer t.Stop()
		for {
			select {
			case <-c.stop:
				return
			case <-t.C:
			}
			if c.st.Setting(keyAutoUpdate) != "false" {
				if _, err := c.installUpdate(); err != nil {
					log.Printf("update: %v", err)
				}
			}
			t.Reset(updateEvery)
		}
	}()
}

// installUpdate 检查并安装新版本，返回装好的版本号（已是最新时为空）。在后台 goroutine 里调用。
func (c *controller) installUpdate() (string, error) {
	u := &c.upd
	u.mu.Lock()
	if u.busy {
		ready := u.ready
		u.mu.Unlock()
		return ready, nil
	}
	u.busy = true
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.busy = false
		u.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	up, err := mygo.Updater.Check(ctx)
	if err != nil || up == nil {
		return "", err
	}
	// Check 和正在运行的版本比较：装好之后、重启之前它仍会报告同一个新版本，不必再装一遍
	if up.Version == u.readyVersion() {
		return up.Version, nil
	}
	if err := up.Install(ctx, nil); err != nil {
		return "", err
	}
	u.mu.Lock()
	u.ready = up.Version
	u.mu.Unlock()
	log.Printf("update: installed %s; it runs at the next launch", up.Version)
	mygo.RunOnMain(c.redrawAll)
	return up.Version, nil
}

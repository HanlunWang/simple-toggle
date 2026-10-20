package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HanlunWang/simple-toggle/internal/engine"
	"github.com/egoist/mygo/ui"
)

// TestReadmeShots 渲染 README 用的截图（英文与中文，默认跳过）：
//
//	OT_README_DIR=docs/images go test -run TestReadmeShots ./app/
func TestReadmeShots(t *testing.T) {
	dir := os.Getenv("OT_README_DIR")
	if dir == "" {
		t.Skip("set OT_README_DIR to render the README screenshots")
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join("..", dir) // go test 在包目录里运行
	}
	defer setLanguage("zh-Hans")
	for _, lang := range []string{"en", "zh-Hans"} {
		setLanguage(lang)
		readmeShots(t, dir, pick(lang == "en", "", ".zh-CN"))
	}
}

func readmeShots(t *testing.T, dir, ext string) {
	save := func(tt *ui.Tester, name string) {
		tt.Advance(time.Second)
		f, err := os.Create(filepath.Join(dir, name+ext+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	for _, dark := range []bool{true, false} {
		suffix := pick(dark, "-dark", "-light")

		// 菜单栏面板：Keep Awake 正在倒计时
		b := &fakeBackend{views: sampleViews()}
		p := newPanel(b, Prefs{Accent: PalGraphite, Layout: "list", Density: "comfortable", ShowGroups: true})
		pt := ui.NewTester(p.view, 340, 300)
		pt.SetScale(2)
		pt.SetDark(dark)
		pt.Frame()
		save(pt, "panel"+suffix)

		// 工作台：编辑一个常驻开关
		w, wb, tt, _ := newTestWorkbench(t)
		tt.SetScale(2)
		tt.SetDark(dark)
		tt.Frame()
		save(tt, "workbench"+suffix)

		// 活动 → 概览
		wb.mu.Lock()
		wb.runs = fakeUsageRuns(time.Now())
		for i := range wb.runs {
			if wb.runs[i].Switch == "scene:focus" && ext == "" {
				wb.runs[i].Name = "Focus"
			}
		}
		wb.mu.Unlock()
		w.handle(engine.Event{Kind: engine.EventRun})
		w.open(secActivity, "")
		w.actTab, w.statRange = 0, 1
		tt.Frame()
		save(tt, "activity"+suffix)
	}
}

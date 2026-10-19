package app

import (
	"embed"
	"log"
	"sync"

	"github.com/egoist/mygo/ui"
)

// 界面字体：Geist（无衬线）、Geist Mono（等宽：标签、数字、代码）、Geist Pixel Circle（点阵：大号数字）。
// 全部是 Vercel 的开源字体（SIL OFL 1.1，见 fonts/OFL.txt）；点阵字只保留了基本拉丁字符。
//
//go:embed fonts/*.ttf
var fontFiles embed.FS

const (
	uiFont   = "Geist, PingFang SC"             // 中文落到苹方
	monoFont = "Geist Mono, PingFang SC"        // 等宽
	dotFont  = "Geist Pixel Circle, Geist Mono" // 点阵数字
)

var fontsOnce sync.Once

// registerFonts 注册内置字体（只做一次；测试与 app 都在第一次画界面前调用）。
func registerFonts() {
	fontsOnce.Do(func() {
		for _, f := range []struct{ file, family string }{
			{"Geist-Regular.ttf", "Geist"}, {"Geist-Medium.ttf", "Geist"}, {"Geist-SemiBold.ttf", "Geist"},
			{"GeistMono-Regular.ttf", "Geist Mono"}, {"GeistMono-Medium.ttf", "Geist Mono"}, {"GeistMono-SemiBold.ttf", "Geist Mono"},
			{"GeistPixel-Circle.ttf", "Geist Pixel Circle"},
		} {
			data, err := fontFiles.ReadFile("fonts/" + f.file)
			if err == nil {
				err = ui.RegisterFont(data, f.family)
			}
			if err != nil {
				log.Println("font", f.file, err)
			}
		}
	})
}

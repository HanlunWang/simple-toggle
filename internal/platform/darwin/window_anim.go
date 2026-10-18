//go:build darwin

package darwin

import (
	"runtime"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// 窗口动画交给 Core Animation：在窗口服务器里逐帧插值，比在 Go 里一步步改透明度平滑得多。

type nsPoint struct{ X, Y float64 }
type nsSize struct{ W, H float64 }
type nsRect struct {
	Origin nsPoint
	Size   nsSize
}

var (
	animOnce        sync.Once
	animErr         error
	msgRect         func(obj objc.ID, sel objc.SEL) nsRect
	msgSetFrame     func(obj objc.ID, sel objc.SEL, r nsRect, display bool)
	msgSetDouble    func(obj objc.ID, sel objc.SEL, v float64)
	msgTimingPoints func(cls objc.Class, sel objc.SEL, a, b, c, d float32) objc.ID
)

func loadAnim() error {
	animOnce.Do(func() {
		objcLib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", rtld)
		if err != nil {
			animErr = err
			return
		}
		if _, err := purego.Dlopen(frameworks+"QuartzCore.framework/QuartzCore", rtld); err != nil {
			animErr = err
			return
		}
		if _, err := purego.Dlopen(frameworks+"AppKit.framework/AppKit", rtld); err != nil {
			animErr = err
			return
		}
		send, err := purego.Dlsym(objcLib, "objc_msgSend")
		if err != nil {
			animErr = err
			return
		}
		stret := send // 大于 16 字节的结构体返回值在 amd64 上走另一个入口
		if runtime.GOARCH == "amd64" {
			if stret, err = purego.Dlsym(objcLib, "objc_msgSend_stret"); err != nil {
				animErr = err
				return
			}
		}
		purego.RegisterFunc(&msgRect, stret)
		purego.RegisterFunc(&msgSetFrame, send)
		purego.RegisterFunc(&msgSetDouble, send)
		purego.RegisterFunc(&msgTimingPoints, send)
	})
	return animErr
}

// RevealWindow 让窗口从上方 dy 点处淡入并落到原位（主线程调用）。
// 曲线接近 iOS 的减速曲线：开始快、收尾很轻。instant 时直接显示。
func RevealWindow(handle uintptr, seconds, dy float64, instant bool) {
	win := objc.ID(handle)
	if win == 0 || loadAnim() != nil || instant {
		if win != 0 && loadAnim() == nil {
			msgSetDouble(win, objc.RegisterName("setAlphaValue:"), 1)
		}
		return
	}
	frame := msgRect(win, objc.RegisterName("frame"))
	start := frame
	start.Origin.Y += dy // Cocoa 的 y 轴向上：先放在上方 dy 点处
	msgSetFrame(win, objc.RegisterName("setFrame:display:"), start, false)
	runAnimation(win, seconds, func(animator objc.ID) {
		msgSetDouble(animator, objc.RegisterName("setAlphaValue:"), 1)
		msgSetFrame(animator, objc.RegisterName("setFrame:display:"), frame, true) // frame 是窗口可动画的属性
	})
}

// FadeOutWindow 让窗口在 seconds 秒内淡出（主线程调用），结束后由调用方关闭窗口。
func FadeOutWindow(handle uintptr, seconds float64) {
	win := objc.ID(handle)
	if win == 0 || loadAnim() != nil {
		return
	}
	runAnimation(win, seconds, func(animator objc.ID) {
		msgSetDouble(animator, objc.RegisterName("setAlphaValue:"), 0)
	})
}

func runAnimation(win objc.ID, seconds float64, body func(animator objc.ID)) {
	ctxCls := objc.ID(objc.GetClass("NSAnimationContext"))
	ctxCls.Send(objc.RegisterName("beginGrouping"))
	ctx := ctxCls.Send(objc.RegisterName("currentContext"))
	msgSetDouble(ctx, objc.RegisterName("setDuration:"), seconds)
	timing := msgTimingPoints(objc.GetClass("CAMediaTimingFunction"), objc.RegisterName("functionWithControlPoints::::"), 0.2, 0.9, 0.3, 1)
	ctx.Send(objc.RegisterName("setTimingFunction:"), timing)
	body(win.Send(objc.RegisterName("animator")))
	ctxCls.Send(objc.RegisterName("endGrouping"))
}

package app

import (
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// 动效：统一的时长与曲线，界面各处共用，保持节奏一致。
// 系统「减弱动态效果」打开时，mygo 会让位置与尺寸的动画直接到位，只保留颜色与淡入淡出。

var (
	animHover = ui.Animation{Duration: 120 * time.Millisecond}
	animColor = ui.Animation{Duration: 180 * time.Millisecond}
	// animKnob 是开关圆钮：带一点回弹的弹簧，中途反向点击会顺势折返
	animKnob  = ui.Animation{Spring: ui.SpringOptions{Duration: 340 * time.Millisecond, Bounce: 0.22}}
	animPress = ui.Animation{Spring: ui.SpringOptions{Duration: 220 * time.Millisecond, Bounce: 0.1}}
	// animSlide 是选中块在条目之间滑动
	animSlide = ui.Animation{Spring: ui.SpringOptions{Duration: 300 * time.Millisecond, Bounce: 0.08}}

	// motionReveal 是展开 / 收起的区块：高度从 0 长出并淡入。
	motionReveal = ui.ElementTransition{
		Duration: 200 * time.Millisecond, Ease: ui.EaseInOut,
		Enter: &ui.Motion{Collapse: true}, Exit: &ui.Motion{Collapse: true},
	}
	// motionRow 是列表行：增删时收拢，换位时滑到新位置。
	motionRow = ui.ElementTransition{
		Duration: 220 * time.Millisecond, Ease: ui.EaseInOut, Position: true,
		Enter: &ui.Motion{Collapse: true}, Exit: &ui.Motion{Collapse: true},
	}
	// motionPage 是切换分区时主区内容的淡入与轻微上移。
	motionPage = ui.ElementTransition{
		Duration: 180 * time.Millisecond,
		Enter:    &ui.Motion{Y: 6},
	}
	motionColors = ui.ElementTransition{Colors: true, Duration: 160 * time.Millisecond}
)

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// pressPose 让可点击的元素按下时轻轻缩小、松开时弹回；只重绘，不重新布局。
func pressPose(e ui.Element, scale float32) ui.Element {
	pose := ui.Pose{}
	if e.Pressed() {
		pose.Scale = scale
	}
	return e.AnimatePose(pose, animPress)
}

// toggle 是 SimpleToggle 自己的开关：轨道颜色随开合渐变，圆钮用弹簧滑动、按下时拉长，
// busy（等待脚本结果）时圆钮里转一个小环。无障碍上仍是一个开关（SwitchBase），名字由调用方给 Label。
func (p palette) toggle(c *ui.Context, key string, on *bool, busy bool, compact bool) ui.Element {
	w, h := float32(38), float32(22)
	if compact {
		w, h = 32, 19
	}
	sw := ui.SwitchBase(c.Key(key), on).Size(w, h).Radius(h / 2).Shrink(0)
	pos := sw.AnimateTo("knob", b2f(*on), animKnob)
	press := sw.AnimateTo("press", b2f(sw.Pressed()), animHover)
	hover := sw.AnimateTo("hover", b2f(sw.Hovered()), animHover)
	onCol := p.Accent
	if p.Ink {
		onCol = p.FG
	}
	offCol := alpha(p.FG, 0.13+0.05*hover)
	sw.Background(offCol.Mix(onCol, max(0, min(pos, 1))))
	knobCol := ui.RGB(255, 255, 255)
	if p.Ink && p.Dark {
		knobCol = knobCol.Mix(p.BG, max(0, min(pos, 1))) // 浅色轨道上用深色圆钮
	}
	var spin float32
	if busy {
		spin = sw.Loop("spin", 900*time.Millisecond, ui.Linear)
	}
	sw.Draw(func(pt *ui.Painter, r ui.Rect) {
		in := float32(2)
		d := r.H - 2*in
		kw := d + 5*press // 按下时圆钮拉长，像 iOS
		x := r.X + in + pos*(r.W-2*in-kw)
		knob := ui.Rect{X: x, Y: r.Y + in, W: kw, H: d}
		pt.Shadow(knob, d/2, 0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.18+0.10*hover))
		pt.Fill(knob, knobCol, d/2)
		if busy {
			var path ui.Path
			cx, cy, rr := knob.X+knob.W/2, knob.Y+knob.H/2, d*0.26
			path.Circle(cx, cy, rr).Trim(spin, spin+0.32)
			pt.StrokePath(&path, 1.6, onCol)
			if spin+0.32 > 1 {
				var wrap ui.Path
				wrap.Circle(cx, cy, rr).Trim(0, spin+0.32-1)
				pt.StrokePath(&wrap, 1.6, onCol)
			}
		}
	})
	return sw
}

// slideMatch 是在一组条目之间滑动的选中块的身份（见 ElementTransition.Match）。
type slideMatch struct{ group string }

// pick 按条件二选一。
func pick[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

// cached 把读文件、查 PATH 之类的检测包一层短时缓存：视图每帧都会调用它们，
// 悬停动画时一秒几十帧，不必每帧都重新读配置文件。
func cached[T any](ttl time.Duration, fn func() T) func() T {
	var (
		mu  sync.Mutex
		at  time.Time
		val T
	)
	return func() T {
		mu.Lock()
		defer mu.Unlock()
		if at.IsZero() || time.Since(at) > ttl {
			val, at = fn(), time.Now()
		}
		return val
	}
}

func cachedSkill(fn func() (bool, string)) func() (bool, string) {
	type pair struct {
		ok     bool
		target string
	}
	get := cached(time.Second, func() pair { ok, t := fn(); return pair{ok, t} })
	return func() (bool, string) { p := get(); return p.ok, p.target }
}

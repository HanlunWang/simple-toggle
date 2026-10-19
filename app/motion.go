package app

import (
	"math"
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

	// animResize 是面板窗口跟着内容变高 / 变矮，与 motionRow 同一节奏
	animResize = ui.Animation{Duration: 220 * time.Millisecond, Ease: ui.EaseInOut}

	// animInstant 让数值直接到位（用来与已在逐帧变化的布局保持同步）
	animInstant = ui.Animation{Duration: time.Nanosecond}

	// motionReveal 是展开 / 收起的区块：高度从 0 长出并淡入。
	motionReveal = ui.ElementTransition{
		Duration: 220 * time.Millisecond, Ease: ui.EaseInOut, Colors: true, // 只管进出场，尺寸跟着布局走
		Enter: &ui.Motion{Collapse: true}, Exit: &ui.Motion{Collapse: true},
	}
	// motionRow 是列表行：增删时收拢，换位时滑到新位置。
	motionRow = ui.ElementTransition{
		Duration: 220 * time.Millisecond, Ease: ui.EaseInOut, Position: true,
		Enter: &ui.Motion{Collapse: true}, Exit: &ui.Motion{Collapse: true},
	}
	// motionRowStill 是只做增删、不做位置过渡的列表行（位置交给正在变化的布局）
	motionRowStill = ui.ElementTransition{
		Duration: 220 * time.Millisecond, Ease: ui.EaseInOut, Colors: true,
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

// toggle 是 SimpleToggle 的开关，延续 0.5 版的「发光拨杆」：描线胶囊，关时是空心圆钮，
// 开时圆钮变成实心的强调色并微微发光，胶囊描边变亮、外圈一层很淡的光晕。
// 圆钮用弹簧滑动、按下时拉长；busy（等脚本结果）时光晕呼吸。无障碍上仍是一个开关。
func (p palette) toggle(c *ui.Context, key string, on *bool, busy bool, compact bool) ui.Element {
	w, h := float32(38), float32(21)
	if compact {
		w, h = 32, 18
	}
	sw := ui.SwitchBase(c.Key(key), on).Size(w, h).Radius(h / 2).Shrink(0)
	pos := sw.AnimateTo("knob", b2f(*on), animKnob)
	lit := max(0, min(sw.AnimateTo("lit", b2f(*on), animColor), 1)) // 颜色与光晕：不回弹
	press := sw.AnimateTo("press", b2f(sw.Pressed()), animHover)
	hover := sw.AnimateTo("hover", b2f(sw.Hovered()), animHover)
	glow := p.glowColor()
	breath := float32(1)
	if busy {
		t := sw.Loop("breath", 1200*time.Millisecond, ui.Linear)
		breath = 0.45 + 0.55*float32(0.5-0.5*math.Cos(2*math.Pi*float64(t)))
	}
	sw.Draw(func(pt *ui.Painter, r ui.Rect) {
		rad := r.H / 2
		// 胶囊：极淡的底、1px 描边；开启时描边换成强调色并带外圈光晕
		if lit > 0 {
			pt.Shadow(r, rad, 0, 0, 9, 0, alpha(glow, 0.24*lit*breath))
		}
		pt.Fill(r, alpha(p.FG, 0.03+0.03*hover).Mix(alpha(glow, 0.14), lit), rad)
		pt.Stroke(r, alpha(p.FG, 0.30+0.12*hover).Mix(alpha(glow, 0.6), lit), rad, 1)
		// 圆钮：关时空心描线，开时实心信号色并发光
		in := float32(4)
		d := r.H - 2*in
		kw := d + 4*press // 按下时拉长
		x := r.X + in + pos*(r.W-2*in-kw)
		knob := ui.Rect{X: x, Y: r.Y + in, W: kw, H: d}
		if lit > 0 {
			pt.Shadow(knob, d/2, 0, 0, 10, 0, alpha(glow, 0.45*lit*breath)) // 外层柔光
			pt.Shadow(knob, d/2, 0, 0, 4, 0, alpha(glow, 0.85*lit*breath))  // 内层亮边
		}
		pt.Fill(knob, alpha(glow, lit), d/2)
		if lit < 1 {
			pt.Stroke(knob, alpha(p.FG, (0.45+0.15*hover)*(1-lit)), d/2, 1)
		}
	})
	return sw
}

// glowColor 是发光的颜色：强调色，墨色主题下是前景色（暗色里是白光）。
func (p palette) glowColor() ui.Color {
	if p.Ink {
		return p.FG
	}
	return p.Accent
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

// noticeText 是顶栏里的临时提示：出现时从左侧淡入，消失时淡出，内容变化时交叉淡化。
func (w *workbench) noticeLabel(c *ui.Context, col ui.Color) ui.Element {
	return ui.Text(c.Key("notice"), w.notice).FontSize(11.5).TextColor(col).SingleLine().MinWidth(0).
		ContentTransition(ui.ContentTransition{}).
		Transition(ui.ElementTransition{Duration: 180 * time.Millisecond, Enter: &ui.Motion{X: -6}, Exit: &ui.Motion{}})
}

// popIn 让弹出层出现时从 96% 放大并淡入（第一次构建时播放一次，只重绘）。
func popIn(e ui.Element) ui.Element {
	k := e.Keyframes("pop", []ui.Keyframe{{Value: 0}, {At: 160 * time.Millisecond, Value: 1, Ease: ui.EaseOut}})
	if k < 1 {
		e.TransformOrigin(0.5, 0).Opacity(k).Scale(0.96+0.04*k, 0.96+0.04*k)
	}
	return e
}

// enterOnce 是元素第一次出现时的入场进度（0→1），delay 用来错开一组元素。
// 只在第一次构建时播放；之后一直是 1，不再请求帧。
func enterOnce(e ui.Element, d, delay time.Duration) float32 {
	return e.Keyframes("enter", []ui.Keyframe{{Value: 0}, {At: d, Value: 1, Ease: ui.EaseOut}}, ui.KeyframeOptions{Delay: delay})
}

// riseIn 让元素第一次出现时淡入并上浮几个点（错开 delay）。
func riseIn(e ui.Element, delay time.Duration) ui.Element {
	if k := enterOnce(e, 320*time.Millisecond, delay); k < 1 {
		e.Opacity(k).Translate(0, (1-k)*8)
	}
	return e
}

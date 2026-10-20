package app

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// 点阵原语（设计语言 v2）：点阵只用在定义性的时刻 —— 状态记号、运行中、Logo、大号数字、热力图。
// 只有 Logo 的 › 出现时会闪亮；其他点阵直接显示，不闪烁、不移动。

// rand01 是可重复的伪随机（0–1），让每个点的时刻错开但每次一样。
func rand01(i, salt int) float64 {
	x := math.Sin(float64(i+1)*12.9898+float64(salt)*78.233) * 43758.5453
	return x - math.Floor(x)
}

// dotRunLayers 是 3×3 九点从外到内的层：四角 0 → 四边 1 → 中心 2。
var dotRunLayers = [9]int{0, 1, 0, 1, 2, 1, 0, 1, 0}

// inward 是「运行中」一个点在一轮里的亮度：亮起 → 停留 → 暗下（与原型的 CSS 关键帧一致）。
func inward(phase float64) float32 {
	switch {
	case phase < 0.12:
		return float32(0.16 + 0.84*phase/0.12)
	case phase < 0.58:
		return 1
	case phase < 0.74:
		return float32(1 - 0.84*(phase-0.58)/0.16)
	}
	return 0.16
}

// dotRun 是「运行中」：3×3 九点点阵，四角 → 四边 → 中心依次亮起再一起暗下，循环（1.4 秒一轮）。
// 动画只在绘制时计算，不重建视图；每秒重绘 20 次就足够平滑，常驻开关开几个小时也不费电。size 是点阵的边长。
func (p palette) dotRun(c *ui.Context, size float32) ui.Element {
	d := size / 3.8
	g := (size - 3*d) / 2
	return ui.Box(c).Size(size, size).Shrink(0).Label(tr("running")).Draw(func(pt *ui.Painter, r ui.Rect) {
		const period = 1400 * time.Millisecond
		t := pt.Now()
		for i, layer := range dotRunLayers {
			el := t.Add(-time.Duration(layer) * 200 * time.Millisecond)
			phase := float64(el.UnixNano()%int64(period)) / float64(period)
			a := inward(phase)
			x, y := r.X+float32(i%3)*(d+g), r.Y+float32(i/3)*(d+g)
			dot := ui.Rect{X: x, Y: y, W: d, H: d}
			if a > 0.5 {
				pt.Shadow(dot, d/2, 0, 0, 3, 0, alpha(p.Accent, 0.3*a))
			}
			pt.Fill(dot, alpha(p.Accent, a), d/2)
		}
		pt.After(50 * time.Millisecond)
	})
}

// mark 是状态记号。工作台（C）：3×3 的小点阵，开启时信号色发光；面板（B）：6px 的小方点。
// state："on"、"run"、"err"、"warn"、"unk" 或空（关）。
func (p palette) mark(c *ui.Context, state string) ui.Element {
	if state == "run" && p.Surface == surfaceWork {
		return p.dotRun(c, 9)
	}
	col, glow := p.L4, false
	switch state {
	case "on":
		col, glow = p.Accent, true
	case "run":
		col, glow = p.Accent, true
	case "err":
		col = p.Red
	case "warn":
		col = p.Amber
	}
	if p.Surface == surfacePanel {
		b := ui.Box(c).Size(6, 6).Radius(1.5).Shrink(0)
		if state == "unk" {
			return b.Border(1, p.L4)
		}
		b.Background(b.AnimateColor("c", col, animColor))
		if glow {
			b.Shadow(0, 0, 6, 0, p.glow())
		}
		return b
	}
	return ui.Box(c).Size(9, 9).Shrink(0).Draw(func(pt *ui.Painter, r ui.Rect) {
		const d, step = 2.3, 3.35
		for i := 0; i < 9; i++ {
			dot := ui.Rect{X: r.X + float32(i%3)*step, Y: r.Y + float32(i/3)*step, W: d, H: d}
			if glow {
				pt.Shadow(dot, d/2, 0, 0, 3, 0, p.glowSoft())
			}
			if state == "unk" {
				pt.Stroke(dot, p.L4, d/2, 0.6)
				continue
			}
			pt.Fill(dot, col, d/2)
		}
	})
}

// chevronDots 是 Logo 的点阵 ›（5 行 × 4 列）。
var chevronDots = []string{"#...", ".#..", "..#.", ".#..", "#..."}

// lampOn 是老式指示灯通电时的亮度曲线：先闪两下再稳定亮起（t 为 0–1）。
func lampOn(t float64) float32 {
	keys := [][2]float64{{0, 0}, {.10, .9}, {.18, .15}, {.30, 1}, {.42, .5}, {.58, 1}, {1, 1}}
	if t <= 0 {
		return 0
	}
	for i := 1; i < len(keys); i++ {
		if t <= keys[i][0] {
			a, b := keys[i-1], keys[i]
			return float32(a[1] + (b[1]-a[1])*(t-a[0])/(b[0]-a[0]))
		}
	}
	return 1
}

// brand 是 Logo：点阵的 › + 等宽小写 simpletoggle。flicker 时 › 出现时像指示灯一样逐点闪亮
// （引导页）；菜单栏面板每次打开都会出现，直接显示。
func (p palette) brand(c *ui.Context, size float32, flicker bool) ui.Element {
	return ui.Row(c.Key("brand")).Gap(size * 0.55).AlignItems(ui.Center).Children(func() {
		dot := size * 0.19
		gap := size * 0.085
		w, h := 4*dot+3*gap, 5*dot+4*gap
		lamp := ui.Box(c.Key("prompt")).Size(w, h).Shrink(0)
		// 一次性的通电动画：0.95 秒内各点按错开的时刻闪亮（只在第一次出现时播放）
		k := float32(1)
		if flicker {
			k = lamp.Keyframes("on", []ui.Keyframe{{Value: 0}, {At: 950 * time.Millisecond, Value: 1, Ease: ui.Linear}})
		}
		lamp.Draw(func(pt *ui.Painter, r ui.Rect) {
			n := 0
			for row, line := range chevronDots {
				for col, ch := range line {
					if ch != '#' {
						continue
					}
					delay := rand01(n, 4) * 0.4
					a := lampOn((float64(k) - delay) / 0.59)
					n++
					d := ui.Rect{X: r.X + float32(col)*(dot+gap), Y: r.Y + float32(row)*(dot+gap), W: dot, H: dot}
					pt.Shadow(d, dot/2, 0, 0, 4, 0, alpha(p.Accent, 0.55*a))
					pt.Fill(d, alpha(p.Accent, a), dot/2)
				}
			}
		})
		ui.Text(c, "simpletoggle").Font(monoFont).FontSize(size).FontWeight(600).LetterSpacing(-0.1).TextColor(p.T1).SingleLine()
	})
}

// dotNum 是点阵大号数字（统计卡片、倒计时），数字变化时滚动。
func (p palette) dotNum(c *ui.Context, key, text string, size float32, col ui.Color) ui.Element {
	return ui.Text(c.Key(key), text).Font(dotFont).FontSize(size).TextColor(col).SingleLine().
		ContentTransition(ui.ContentTransition{Numeric: true})
}

// meter 是面板倒计时旁的进度格（B）：n 个小方格，剩余部分是信号色并微微发光。
func (p palette) meter(c *ui.Context, value float32, n int) ui.Element {
	f := int(math.Round(float64(value) * float64(n)))
	return ui.Box(c).Size(float32(n)*7-2, 9).Shrink(0).Draw(func(pt *ui.Painter, r ui.Rect) {
		for i := 0; i < n; i++ {
			cell := ui.Rect{X: r.X + float32(i)*7, Y: r.Y, W: 5, H: 9}
			if i < f {
				pt.Shadow(cell, 1, 0, 0, 4, 0, p.glowSoft())
				pt.Fill(cell, p.Accent, 1)
			} else {
				pt.Fill(cell, p.L3, 1)
			}
		}
	})
}

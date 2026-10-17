package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// 菜单栏主图标：程序化绘制的模板图（黑色、透明底，系统按菜单栏明暗着色），@2x 像素。
//
//	pill  胶囊 + 旋钮：全关时空心旋钮在左，有开关运行时实心旋钮在右
//	dot   圆环：有开关运行时中间填实
//	bars  三根竖条：开启数量 0..3+ 逐根点亮（未点亮的画成细线）
//
// busy（有开关正在执行）时旋钮 / 圆点缩小一圈，配合定时重绘形成轻微的脉动。
type trayStyle string

const (
	trayPill trayStyle = "pill"
	trayDot  trayStyle = "dot"
	trayBars trayStyle = "bars"
)

func trayIconPNG(style trayStyle, onCount int, busyPhase bool) []byte {
	switch style {
	case trayDot:
		return drawDot(onCount > 0, busyPhase)
	case trayBars:
		return drawBars(onCount)
	}
	return drawPill(onCount > 0, busyPhase)
}

type canvas struct {
	img  *image.NRGBA
	w, h int
}

func newCanvas(w, h int) *canvas {
	return &canvas{img: image.NewNRGBA(image.Rect(0, 0, w, h)), w: w, h: h}
}

// fill 以 4×4 超采样给覆盖区域上色，边缘抗锯齿。
func (cv *canvas) fill(inside func(x, y float64) bool) {
	const ss = 4
	for y := 0; y < cv.h; y++ {
		for x := 0; x < cv.w; x++ {
			n := 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					if inside(float64(x)+(float64(sx)+0.5)/ss, float64(y)+(float64(sy)+0.5)/ss) {
						n++
					}
				}
			}
			if n == 0 {
				continue
			}
			a := uint8(math.Min(255, float64(cv.img.NRGBAAt(x, y).A)+float64(n)*255/(ss*ss)))
			cv.img.SetNRGBA(x, y, color.NRGBA{A: a})
		}
	}
}

func (cv *canvas) png() []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, cv.img)
	return b.Bytes()
}

func inCapsule(x, y, x0, x1, cy, r float64) bool {
	cx := math.Min(math.Max(x, x0+r), x1-r)
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

func inCircle(x, y, cx, cy, r float64) bool {
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

func drawPill(on, busy bool) []byte {
	const w, h = 46, 30
	cv := newCanvas(w, h)
	cy := h / 2.0
	cv.fill(func(x, y float64) bool {
		return inCapsule(x, y, 2, w-2, cy, 12) && !inCapsule(x, y, 5.2, w-5.2, cy, 8.8)
	})
	kx, r := 14.0, 6.2
	if on {
		kx = w - 14.0
	}
	if busy {
		r = 4.6
	}
	cv.fill(func(x, y float64) bool {
		if on {
			return inCircle(x, y, kx, cy, r)
		}
		return inCircle(x, y, kx, cy, r) && !inCircle(x, y, kx, cy, r-2.2)
	})
	return cv.png()
}

func drawDot(on, busy bool) []byte {
	const s = 30
	cv := newCanvas(s, s)
	c := s / 2.0
	cv.fill(func(x, y float64) bool { return inCircle(x, y, c, c, 12) && !inCircle(x, y, c, c, 8.8) })
	if on {
		r := 5.0
		if busy {
			r = 3.4
		}
		cv.fill(func(x, y float64) bool { return inCircle(x, y, c, c, r) })
	}
	return cv.png()
}

func drawBars(onCount int) []byte {
	const w, h = 30, 30
	cv := newCanvas(w, h)
	for i := 0; i < 3; i++ {
		x0 := 5.0 + float64(i)*8
		lit := onCount > i
		cv.fill(func(x, y float64) bool {
			if lit {
				return inCapsule(y, x, 5, h-5, x0+2.5, 2.5) // 竖直胶囊：交换坐标
			}
			return inCapsule(y, x, 5, h-5, x0+2.5, 2.5) && !inCapsule(y, x, 7, h-7, x0+2.5, 1.1)
		})
	}
	return cv.png()
}

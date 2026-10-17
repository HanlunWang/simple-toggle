package app

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"

	"github.com/HanlunWang/open-toggle/internal/contract"
)

// lucide.json.gz 是 lucide-static 0.469.0 的全部图标（名称 → 精简 SVG），ISC 许可，见 icons/LUCIDE_LICENSE。
//
//go:embed icons/lucide.json.gz
var lucideGz []byte

var (
	lucideOnce sync.Once
	lucideSrc  map[string]string
	svgMu      sync.Mutex
	svgCache   = map[string]*ui.SVG{}
)

func lucide() map[string]string {
	lucideOnce.Do(func() {
		lucideSrc = map[string]string{}
		r, err := gzip.NewReader(bytes.NewReader(lucideGz))
		if err != nil {
			return
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return
		}
		_ = json.Unmarshal(data, &lucideSrc)
	})
	return lucideSrc
}

// HasIcon 报告 Lucide 里有没有这个名字。
func HasIcon(name string) bool { _, ok := lucide()[name]; return ok }

// svgIcon 返回解析好的 Lucide 图标；名字不存在时用通用图标。
func svgIcon(name string) *ui.SVG {
	svgMu.Lock()
	defer svgMu.Unlock()
	if s, ok := svgCache[name]; ok {
		return s
	}
	src, ok := lucide()[name]
	if !ok {
		src = lucide()["toggle-right"]
	}
	s, err := ui.ParseSVG([]byte(src))
	if err != nil {
		return nil
	}
	svgCache[name] = s
	return s
}

// icon 画一个图标：Lucide 名称，或契约里的写法（lucide: / sf: / emoji）。
func icon(c *ui.Context, name string, size float32) ui.Element {
	if s := svgIcon(name); s != nil {
		return ui.Icon(c, s).FontSize(size)
	}
	return ui.Text(c, "•").FontSize(size)
}

func switchIcon(c *ui.Context, spec string, size float32) ui.Element {
	kind, val := contract.ResolveIcon(spec)
	if kind == contract.IconLucide {
		return icon(c, val, size)
	}
	return ui.Text(c, val).FontSize(size - 1).SingleLine()
}

// templatePNG 把 Lucide 图标光栅化成菜单栏用的模板图（黑色线条、透明底，系统负责着色）。
// px 是像素边长（菜单栏图标 16pt，@2x 即 32）。
func templatePNG(name string, px int) []byte {
	src, ok := lucide()[name]
	if !ok {
		return nil
	}
	src = strings.ReplaceAll(src, "currentColor", "#000000")
	icon, err := oksvg.ReadIconStream(strings.NewReader(src), oksvg.IgnoreErrorMode)
	if err != nil {
		return nil
	}
	icon.SetTarget(0, 0, float64(px), float64(px))
	img := image.NewRGBA(image.Rect(0, 0, px, px))
	scanner := rasterx.NewScannerGV(px, px, img, img.Bounds())
	icon.Draw(rasterx.NewDasher(px, px, scanner), 1)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

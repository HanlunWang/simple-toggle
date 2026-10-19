package app

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"

	"github.com/HanlunWang/simple-toggle/internal/store"
)

// 活动 → 概览：用量统计。数据来自 store 按天汇总的 usage.json（保留 400 天），
// 与运行记录的条数上限无关。图表与热力图各用一个 Draw 画完，悬停只重绘、不重新布局。

var statRanges = []int{7, 30, 90, 365}

// statsView 是当前范围（和选中的开关）下算好的数字。
type statsView struct {
	from, to  time.Time // 含首尾的日期（本地零点）
	days      []time.Time
	counts    []int // 与 days 对应
	total     int
	prevTotal int // 前一段同样长度的总数
	failed    int // 只在不筛选开关时有意义
	top       []store.UsageItem
	allTotal  int // 不筛选开关时的总数（算占比）
	hours     [7][24]int
	sources   map[string]int
	any       bool // 有没有任何统计数据
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// mondayIndex 把 Weekday 换成周一为 0 的序号。
func mondayIndex(d time.Weekday) int { return (int(d) + 6) % 7 }

func (w *workbench) loadUsage() {
	if !w.usageDirty {
		return
	}
	w.usageDirty = false
	w.usage = w.b.Usage()
}

// itemCount 是某一天里选中开关（或全部）的次数。
func (w *workbench) itemCount(d store.UsageDay) int {
	if w.statItem == "" {
		return d.Total
	}
	return d.Items[w.statItem]
}

func (w *workbench) computeStats(now time.Time) statsView {
	n := statRanges[w.statRange]
	sv := statsView{to: dayStart(now), sources: map[string]int{}, any: len(w.usage.Days) > 0}
	sv.from = sv.to.AddDate(0, 0, -(n - 1))
	for i := 0; i < n; i++ {
		day := sv.from.AddDate(0, 0, i)
		d := w.usage.Days[day.Format(store.DayLayout)]
		c := w.itemCount(d)
		sv.days = append(sv.days, day)
		sv.counts = append(sv.counts, c)
		sv.total += c
		sv.allTotal += d.Total
		sv.failed += d.Failed
		wd := mondayIndex(day.Weekday())
		for h, v := range d.Hours {
			sv.hours[wd][h] += v
		}
		for src, v := range d.Source {
			sv.sources[src] += v
		}
	}
	for i := 1; i <= n; i++ {
		sv.prevTotal += w.itemCount(w.usage.Days[sv.from.AddDate(0, 0, -i).Format(store.DayLayout)])
	}
	sv.top = w.usage.Top(sv.from, sv.to)
	return sv
}

// statName 是统计里一个条目的显示名：现有开关用当前名字，场景与删掉的开关用记录里的名字。
func (w *workbench) statName(id string) string {
	if v := w.view(id); v != nil {
		return v.Name
	}
	if n := w.usage.Names[id]; n != "" {
		return n
	}
	return id
}

func (w *workbench) statIcon(c *ui.Context, id string, size float32) {
	pal := w.pal
	switch v := w.view(id); {
	case v != nil:
		switchIcon(c, v.Icon, size).TextColor(pal.T2)
	case strings.HasPrefix(id, "scene:"):
		icon(c, "layers", size).TextColor(pal.T2)
	default:
		icon(c, "file-x", size).TextColor(pal.T3)
	}
}

// heat 是热力图的颜色：0 次为浅底，其余按与最大值的比例分 4 级。
func (p palette) heat(n, hi int) ui.Color {
	if n <= 0 || hi <= 0 {
		return p.L2
	}
	base := p.Accent
	if p.Ink {
		base = p.FG
	}
	level := int(math.Ceil(4 * float64(n) / float64(hi)))
	return alpha(base, []float32{0.22, 0.42, 0.66, 0.92}[min(max(level, 1), 4)-1])
}

func (w *workbench) statsPage(c *ui.Context) {
	pal := w.pal
	w.loadUsage()
	sv := w.computeStats(c.Now())
	ui.Scroll(c.Key("stats-scroll")).FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Padding(18, 20, 28).Gap(14).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, tr("st_title")).FontSize(15).FontWeight(600).TextColor(pal.T1)
				if w.statItem != "" {
					chip := ui.Row(c.Key("st-item")).Height(24).Padding(0, 6, 0, 9).Radius(999).Gap(5).AlignItems(ui.Center).
						Background(pal.L2).Label(tr("st_clear_item")).Transition(ui.ElementTransition{Enter: &ui.Motion{Scale: 0.9}})
					chip.Children(func() {
						w.statIcon(c, w.statItem, 12)
						ui.Text(c, w.statName(w.statItem)).FontSize(12).TextColor(pal.T1).SingleLine()
						icon(c, "x", 12).TextColor(pal.T3)
					})
					if chip.Clicked() {
						w.statItem = ""
					}
				}
				ui.Spacer(c)
				labels := make([]string, len(statRanges))
				for i, n := range statRanges {
					labels[i] = tr(fmt.Sprintf("st_range_%d", n))
				}
				ui.Row(c).Width(260).Children(func() {
					w.statRange = pal.segmented(c, "st-range", w.statRange, labels...)
				})
			})
			if !sv.any {
				ui.Column(c).FillWidth().Padding(48, 20).AlignItems(ui.Center).Gap(8).Radius(6).Border(1, pal.L2).Children(func() {
					icon(c, "bar-chart-3", 22).TextColor(pal.T3)
					ui.Text(c, tr("st_empty")).FontSize(12.5).TextColor(pal.T3).Wrap().TextAlign(ui.Center)
				})
				return
			}
			w.statKPIs(c, sv)
			w.statDaily(c, sv)
			w.statCalendar(c, c.Now())
			ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Stretch).Children(func() {
				ui.Column(c).Grow(3).Basis(0).MinWidth(0).Children(func() { w.statHours(c, sv) })
				ui.Column(c).Grow(2).Basis(0).MinWidth(0).Children(func() { w.statSources(c, sv) })
			})
			w.statRanking(c, sv)
		})
	})
}

// statCard 是统计页的卡片：标题、右侧附注、内容。
func (w *workbench) statCard(c *ui.Context, key, title, note string, body func()) {
	pal := w.pal
	ui.Column(c.Key(key)).FillWidth().Padding(14, 16, 16).Gap(12).Radius(6).Border(1, pal.L2).Background(alpha(pal.FG, 0.015)).
		Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				pal.caption(c, title)
				ui.Spacer(c)
				if note != "" {
					ui.Text(c.Key("note"), note).FontSize(11.5).TextColor(pal.T3).SingleLine().ContentTransition(ui.ContentTransition{})
				}
			})
			body()
		})
}

func (w *workbench) statKPIs(c *ui.Context, sv statsView) {
	pal := w.pal
	n := len(sv.days)
	nk := 0
	kpi := func(key, label, value, sub string, subCol ui.Color) {
		card := ui.Column(c.Key(key)).Grow(1).Basis(0).MinWidth(0).Padding(13, 14).Gap(7).Radius(pal.rCard()).Border(1, pal.L2).Background(alpha(pal.FG, 0.015))
		riseIn(card, time.Duration(nk)*45*time.Millisecond)
		nk++
		card.Children(func() {
			pal.caption(c, label)
			pal.dotNum(c, "v", value, 30, pal.T1).Ellipsis("…") // 点阵大号数字
			ui.Text(c.Key("s"), sub).Font(monoFont).FontSize(10.5).TextColor(subCol).SingleLine().Ellipsis("…").ContentTransition(ui.ContentTransition{})
		})
	}
	ui.Row(c.Key("kpis")).FillWidth().Gap(10).AlignItems(ui.Stretch).Children(func() {
		// 使用次数，与前一段比较
		sub, col := tr("st_no_prev"), pal.T3
		if sv.prevTotal > 0 {
			pct := (float64(sv.total) - float64(sv.prevTotal)) / float64(sv.prevTotal) * 100
			sign := "+"
			col = pal.Green
			if pct < 0 {
				sign, col = "", pal.Red
			}
			sub = fmt.Sprintf(tr("st_vs_prev"), sign+fmt.Sprintf("%.0f%%", pct), n)
		}
		kpi("k-total", tr("st_total"), itoa(sv.total), sub, col)
		kpi("k-avg", tr("st_daily_avg"), fmt.Sprintf("%.1f", float64(sv.total)/float64(n)), fmt.Sprintf(tr("st_active_days"), activeDays(sv.counts), n), pal.T3)
		if w.statItem == "" {
			top, sub := "—", ""
			if len(sv.top) > 0 {
				top, sub = w.statName(sv.top[0].ID), fmt.Sprintf(tr("st_n_times"), sv.top[0].Count)
			}
			kpi("k-top", tr("st_top"), top, sub, pal.T3)
			rate, sub := "—", ""
			if sv.total > 0 {
				rate = fmt.Sprintf("%.0f%%", 100*float64(sv.total-sv.failed)/float64(sv.total))
				sub = fmt.Sprintf(tr("st_failed_n"), sv.failed)
			}
			kpi("k-ok", tr("st_success"), rate, sub, pick(sv.failed > 0, pal.Red, pal.T3))
		} else {
			share := "—"
			if sv.allTotal > 0 {
				share = fmt.Sprintf("%.0f%%", 100*float64(sv.total)/float64(sv.allTotal))
			}
			kpi("k-share", tr("st_share"), share, fmt.Sprintf(tr("st_of_all"), sv.allTotal), pal.T3)
			peak, sub := "—", ""
			if i := argmax(sv.counts); i >= 0 && sv.counts[i] > 0 {
				peak = sv.days[i].Format("01-02")
				sub = fmt.Sprintf(tr("st_n_times"), sv.counts[i])
			}
			kpi("k-peak", tr("st_peak"), peak, sub, pal.T3)
		}
	})
}

func activeDays(counts []int) int {
	n := 0
	for _, c := range counts {
		if c > 0 {
			n++
		}
	}
	return n
}

func argmax(v []int) int {
	best := -1
	for i, x := range v {
		if best < 0 || x > v[best] {
			best = i
		}
	}
	return best
}

// statDaily 是每天（一年范围时每周）的次数柱状图。
func (w *workbench) statDaily(c *ui.Context, sv statsView) {
	pal := w.pal
	type bar struct {
		key, label, tip string
		n               int
	}
	var bars []bar
	weekly := len(sv.days) > 120
	if weekly {
		for i := 0; i < len(sv.days); {
			start := sv.days[i]
			j, sum := i, 0
			for ; j < len(sv.days) && (j == i || mondayIndex(sv.days[j].Weekday()) != 0); j++ {
				sum += sv.counts[j]
			}
			bars = append(bars, bar{start.Format(store.DayLayout), start.Format("01-02"),
				fmt.Sprintf(tr("st_week_tip"), start.Format("01-02"), sum), sum})
			i = j
		}
	} else {
		for i, d := range sv.days {
			bars = append(bars, bar{d.Format(store.DayLayout), d.Format("01-02"),
				fmt.Sprintf(tr("st_day_tip"), d.Format("01-02"), weekdayName(d.Weekday()), sv.counts[i]), sv.counts[i]})
		}
	}
	title := pick(weekly, tr("st_weekly"), tr("st_daily"))
	note := ""
	if w.statHover != "" && w.statHoverKey == "bar" {
		note = w.statHover
	}
	hoverNote := ""
	w.statCard(c, "c-daily", title, note, func() {
		labels := make([]string, len(bars))
		hi := 0
		for i, b := range bars {
			labels[i] = b.label
			hi = max(hi, b.n)
		}
		x := chart.NewBand(labels...).Padding(pick(len(bars) > 40, float32(0.18), 0.3), 0.05)
		y := chart.NewLinear(0, float64(max(hi, 4))).Nice(3)
		left := y.Ticks(3)
		var bottom []chart.Tick
		step := max(1, len(bars)/7)
		for i := len(bars) - 1; i >= 0; i -= step { // 从最近一天往前取，最后一天总有标签
			bottom = append(bottom, chart.Tick{At: x.At(i), Label: bars[i].label})
		}
		label := func(side chart.Side, t chart.Tick) {
			ui.Text(c, t.Label).NoWrap().FontSize(10.5).TextColor(pal.T3)
		}
		col := pal.Accent
		if pal.Ink {
			col = pal.FG
		}
		chart.Plot(c.Key("daily-plot"), chart.Axes{Left: left, Bottom: bottom, Label: label}, func(area ui.Element) {
			area.Draw(func(p *ui.Painter, r ui.Rect) {
				for _, t := range left {
					p.Line(r.X, chart.Y(r, t.At), r.X+r.W, chart.Y(r, t.At), 1, pal.L2)
				}
			})
			for i, b := range bars {
				if b.n == 0 {
					continue
				}
				// 点阵柱：柱子由一格格圆点堆成（C），直接显示
				bc := chart.Rect(c.Key("b-"+b.key), x.Band(i), y.Span(0, float64(b.n))).Label(b.tip).
					Transition(ui.ElementTransition{Duration: 280 * time.Millisecond, Ease: ui.EaseInOut})
				hovered := bc.Hovered()
				dotCol := bc.AnimateColor("c", alpha(col, pick(hovered, float32(1), 0.8)), animHover)
				bc.Draw(func(p *ui.Painter, r ui.Rect) {
					const pitch, d = 4.0, 2.8
					cols := max(1, int((r.W+pitch-d)/pitch))
					off := (r.W - (float32(cols-1)*pitch + d)) / 2
					for yy := r.Y + r.H - d; yy >= r.Y-0.5; yy -= pitch {
						for k := 0; k < cols; k++ {
							p.Fill(ui.Rect{X: r.X + off + float32(k)*pitch, Y: yy, W: d, H: d}, dotCol, d/2)
						}
					}
				})
				if hovered {
					hoverNote = b.tip
				}
			}
		}).Height(168).GapX(8).GapY(6)
	})
	w.setHover(c, "bar", hoverNote)
}

// setHover 记下某个图表这一帧悬停的说明（显示在卡片标题右侧，下一帧生效）。
func (w *workbench) setHover(c *ui.Context, key, text string) {
	old := w.statHover
	switch {
	case text != "":
		w.statHover, w.statHoverKey = text, key
	case w.statHoverKey == key:
		w.statHover, w.statHoverKey = "", ""
	}
	if w.statHover != old {
		c.Invalidate() // 标题右侧的说明在卡片之前画好了：再画一帧显示新的
	}
}

func weekdayName(d time.Weekday) string { return tr(fmt.Sprintf("wd_%d", mondayIndex(d))) }

// statCalendar 是最近 53 周的日历热力图（一列一周，周一在上）。
func (w *workbench) statCalendar(c *ui.Context, now time.Time) {
	pal := w.pal
	today := dayStart(now)
	start := today.AddDate(0, 0, -mondayIndex(today.Weekday())-52*7)
	const weeks = 53
	counts := make([]int, weeks*7)
	total, hi := 0, 0
	for i := range counts {
		d := start.AddDate(0, 0, i)
		if d.After(today) {
			counts[i] = -1
			continue
		}
		n := w.itemCount(w.usage.Days[d.Format(store.DayLayout)])
		counts[i], total, hi = n, total+n, max(hi, n)
	}
	note := fmt.Sprintf(tr("st_year_total"), total)
	if w.statHoverKey == "cal" {
		note = w.statHover
	}
	hoverNote := ""
	w.statCard(c, "c-cal", tr("st_calendar"), note, func() {
		const labelW, top, gap = float32(22), float32(16), float32(3)
		box := ui.Box(c.Key("cal")).FillWidth().Height(top + 7*13 + 6*gap + 2)
		if k := enterOnce(box, 400*time.Millisecond, 120*time.Millisecond); k < 1 {
			box.Opacity(k)
		}
		cellOf := func(r ui.Rect) float32 { return min(13, (r.W-labelW)/weeks-gap) }
		if px, py, over := box.PointerPosition(); over {
			r := box.Bounds()
			cell := cellOf(ui.Rect{W: r.W})
			col := int((px - labelW) / (cell + gap))
			row := int((py - top) / (cell + gap))
			if px >= labelW && py >= top && col >= 0 && col < weeks && row >= 0 && row < 7 {
				if i := col*7 + row; counts[i] >= 0 {
					d := start.AddDate(0, 0, i)
					hoverNote = fmt.Sprintf(tr("st_day_tip"), d.Format("2006-01-02"), weekdayName(d.Weekday()), counts[i])
				}
			}
		}
		box.Draw(func(p *ui.Painter, r ui.Rect) {
			cell := cellOf(r)
			for _, row := range []int{0, 2, 4} {
				p.Text(r.X, r.Y+top+float32(row)*(cell+gap)-1, tr(fmt.Sprintf("wd_%d", row)), 9.5, pal.T3)
			}
			lastMonth := -1
			for col := 0; col < weeks; col++ {
				d := start.AddDate(0, 0, col*7)
				x := r.X + labelW + float32(col)*(cell+gap)
				if m := int(d.Month()); m != lastMonth {
					if lastMonth != -1 || d.Day() <= 7 {
						p.Text(x, r.Y, monthName(d.Month()), 9.5, pal.T3)
					}
					lastMonth = m
				}
				for row := 0; row < 7; row++ {
					n := counts[col*7+row]
					if n < 0 {
						continue
					}
					p.Fill(ui.Rect{X: x, Y: r.Y + top + float32(row)*(cell+gap), W: cell, H: cell}, pal.heat(n, hi), cell/2)
				}
			}
		})
		w.heatLegend(c, hi)
	})
	w.setHover(c, "cal", hoverNote)
}

func monthName(m time.Month) string { return tr(fmt.Sprintf("mon_%d", int(m))) }

// heatLegend 是热力图下方「少 ▢▢▢▢▢ 多」的图例。
func (w *workbench) heatLegend(c *ui.Context, hi int) {
	pal := w.pal
	ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
		ui.Spacer(c)
		ui.Text(c, tr("st_less")).FontSize(10.5).TextColor(pal.T3)
		for _, n := range []int{0, 1, 2, 3, 4} {
			ui.Box(c).Size(10, 10).Radius(5).Background(pal.heat(n, 4))
		}
		ui.Text(c, tr("st_more")).FontSize(10.5).TextColor(pal.T3)
	})
}

// statHours 是「星期 × 小时」的热力图（当前范围，全部开关）。
func (w *workbench) statHours(c *ui.Context, sv statsView) {
	pal := w.pal
	hi := 0
	for _, row := range sv.hours {
		for _, n := range row {
			hi = max(hi, n)
		}
	}
	note := pick(w.statItem != "", tr("st_hours_all"), "")
	if w.statHoverKey == "hours" {
		note = w.statHover
	}
	hoverNote := ""
	w.statCard(c, "c-hours", tr("st_hours"), note, func() {
		const labelW, top, gap = float32(22), float32(14), float32(2)
		box := ui.Box(c.Key("hours")).FillWidth().Height(top + 7*14 + 6*gap)
		if k := enterOnce(box, 400*time.Millisecond, 180*time.Millisecond); k < 1 {
			box.Opacity(k)
		}
		cellW := func(r ui.Rect) float32 { return (r.W - labelW - 23*gap) / 24 }
		if px, py, over := box.PointerPosition(); over {
			cw := cellW(ui.Rect{W: box.Bounds().W})
			col, row := int((px-labelW)/(cw+gap)), int((py-top)/(14+gap))
			if px >= labelW && py >= top && col >= 0 && col < 24 && row >= 0 && row < 7 {
				hoverNote = fmt.Sprintf(tr("st_hour_tip"), tr(fmt.Sprintf("wd_%d", row)), col, col+1, sv.hours[row][col])
			}
		}
		box.Draw(func(p *ui.Painter, r ui.Rect) {
			cw := cellW(r)
			for _, h := range []int{0, 6, 12, 18} {
				p.Text(r.X+labelW+float32(h)*(cw+gap), r.Y, fmt.Sprintf("%d", h), 9.5, pal.T3)
			}
			for row := 0; row < 7; row++ {
				y := r.Y + top + float32(row)*(14+gap)
				if row%2 == 0 {
					p.Text(r.X, y+1, tr(fmt.Sprintf("wd_%d", row)), 9.5, pal.T3)
				}
				for h := 0; h < 24; h++ { // 圆点：取格子里能放下的最大圆
					d := min(cw, 14)
					p.Fill(ui.Rect{X: r.X + labelW + float32(h)*(cw+gap) + (cw-d)/2, Y: y + (14-d)/2, W: d, H: d}, pal.heat(sv.hours[row][h], hi), d/2)
				}
			}
		})
	})
	w.setHover(c, "hours", hoverNote)
}

// sourceShades 是来源分布的单色深浅：次数最多的来源最亮，往后逐级变淡。
var sourceShades = []float32{1, 0.72, 0.52, 0.38, 0.28, 0.2, 0.14}

func (p palette) sourceShade(rank int) ui.Color {
	return alpha(p.Accent, sourceShades[min(rank, len(sourceShades)-1)])
}

// statSources 是按来源的分布：一条堆叠条 + 图例。
func (w *workbench) statSources(c *ui.Context, sv statsView) {
	pal := w.pal
	var order []string
	total := 0
	for _, s := range sourceOrder {
		if sv.sources[s] > 0 {
			order = append(order, s)
			total += sv.sources[s]
		}
	}
	for s, n := range sv.sources { // 未知来源放最后
		if n > 0 && !contains(order, s) {
			order = append(order, s)
			total += n
		}
	}
	// 按次数从多到少排，深浅随名次（相同次数保持原顺序）
	sort.SliceStable(order, func(i, j int) bool { return sv.sources[order[i]] > sv.sources[order[j]] })
	w.statCard(c, "c-src", tr("st_sources"), pick(w.statItem != "", tr("st_hours_all"), ""), func() {
		bar := ui.Box(c.Key("src-bar")).FillWidth().Height(10).Radius(5).Clip().Background(pal.L2)
		bar.Draw(func(p *ui.Painter, r ui.Rect) {
			if total == 0 {
				return
			}
			x := r.X
			for i, s := range order {
				wd := r.W * float32(sv.sources[s]) / float32(total)
				p.Fill(ui.Rect{X: x, Y: r.Y, W: max(wd-1.5, 1), H: r.H}, pal.sourceShade(i), 0)
				x += wd
			}
		})
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			for i, s := range order {
				n := sv.sources[s]
				ui.Row(c.Key("src-" + s)).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(8, 8).Radius(4).Background(pal.sourceShade(i))
					ui.Text(c, sourceLabel(s)).FontSize(12).TextColor(pal.T1).Grow(1).SingleLine()
					pal.mono(c, itoa(n), 11.5, pal.T2)
					pal.mono(c, fmt.Sprintf("%3.0f%%", 100*float64(n)/float64(max(total, 1))), 11, pal.T3)
				})
			}
		})
	})
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// statRanking 是各开关的使用次数排行；点一行只看这个开关，再点一次取消。
func (w *workbench) statRanking(c *ui.Context, sv statsView) {
	pal := w.pal
	const limit = 12
	w.statCard(c, "c-rank", tr("st_rank"), fmt.Sprintf(tr("st_rank_note"), len(sv.top)), func() {
		if len(sv.top) == 0 {
			pal.hint(c, tr("st_none_in_range"))
			return
		}
		hi := sv.top[0].Count
		col := pal.Accent
		if pal.Ink {
			col = pal.FG
		}
		ui.Column(c.Key("rank")).FillWidth().Gap(2).Children(func() {
			for i, it := range sv.top {
				if i >= limit {
					break
				}
				it := it
				sel := w.statItem == it.ID
				r := ui.Row(c.Key("rk-"+it.ID)).FillWidth().Height(32).Padding(0, 8).Radius(4).Gap(10).AlignItems(ui.Center).
					Label(w.statName(it.ID)).Transition(motionRow)
				r.Background(r.AnimateColor("bg", pick(sel, pal.L2, pick(r.Hovered(), pal.L1, ui.Transparent)), animHover))
				r.Children(func() {
					pal.mono(c, itoa(i+1), 11, pal.T3).Width(16)
					ui.Row(c).Width(16).Justify(ui.Center).Children(func() { w.statIcon(c, it.ID, 14) })
					ui.Text(c, w.statName(it.ID)).FontSize(12.5).TextColor(pal.T1).SingleLine().Ellipsis("…").Width(170)
					frac := float32(it.Count) / float32(max(hi, 1))
					// 点阵条：一排 30 个点，亮着的点数按比例
					track := ui.Box(c).Grow(1).Height(6)
					lit := int(math.Round(float64(frac) * 30))
					track.Draw(func(p *ui.Painter, rr ui.Rect) {
						const n = 30
						pitch := rr.W / n
						d := min(pitch-1.5, 5)
						for k := 0; k < n; k++ {
							c := pal.L2
							if k < lit {
								c = alpha(col, pick(sel, float32(1), 0.75))
								if sel {
									p.Shadow(ui.Rect{X: rr.X + float32(k)*pitch, Y: rr.Y + (rr.H-d)/2, W: d, H: d}, d/2, 0, 0, 4, 0, pal.glowSoft())
								}
							}
							p.Fill(ui.Rect{X: rr.X + float32(k)*pitch, Y: rr.Y + (rr.H-d)/2, W: d, H: d}, c, d/2)
						}
					})
					pal.mono(c, itoa(it.Count), 11.5, pal.T1).Width(44).TextAlign(ui.End)
					pal.mono(c, fmt.Sprintf("%.0f%%", 100*float64(it.Count)/float64(max(sv.allTotal, 1))), 11, pal.T3).Width(40).TextAlign(ui.End)
				})
				if r.Clicked() {
					w.statItem = pick(sel, "", it.ID)
				}
			}
			if len(sv.top) > limit {
				rest := 0
				for _, it := range sv.top[limit:] {
					rest += it.Count
				}
				pal.hint(c, fmt.Sprintf(tr("st_others"), len(sv.top)-limit, rest)).Margin(6, 8, 0)
			}
		})
	})
}

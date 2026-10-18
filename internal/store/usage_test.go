package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUsageCountsAndPersists(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, r := range []Run{
		{Switch: "a.sh", Name: "A", Action: "on", Source: "manual", OK: true, Time: now},
		{Switch: "a.sh", Name: "A", Action: "off", Source: "shortcut", OK: true, Time: now},
		{Switch: "b.sh", Name: "B", Action: "run", Source: "ai", Time: now}, // 失败也算一次使用
		{Switch: "a.sh", Name: "A", Action: "status", Source: "manual", OK: true, Time: now},
		{Switch: "a.sh", Name: "A", Action: "on", Source: "restore", OK: true, Time: now},
		{Switch: "c.sh", Name: "C", Action: "exit", Source: "system", Time: now},
	} {
		if _, err := s.AppendRun(r); err != nil {
			t.Fatal(err)
		}
	}
	u := s.Usage()
	d := u.Days[now.Format(DayLayout)]
	if d.Total != 3 || d.Failed != 1 || d.Items["a.sh"] != 2 || d.Items["b.sh"] != 1 || d.Hours[now.Hour()] != 3 || d.Source["ai"] != 1 {
		t.Fatalf("day = %+v", d)
	}
	top := u.Top(now.AddDate(0, 0, -7), now)
	if len(top) != 2 || top[0].ID != "a.sh" || top[0].Name != "A" || top[0].Count != 2 {
		t.Fatalf("top = %+v", top)
	}
	// 重新打开：从 usage.json 读回
	s2, _ := Open(dir)
	if s2.Usage().Days[now.Format(DayLayout)].Total != 3 {
		t.Fatal("usage not persisted")
	}
	// 清空记录也清空统计
	if err := s2.ClearRuns(); err != nil {
		t.Fatal(err)
	}
	if len(s2.Usage().Days) != 0 {
		t.Fatal("usage not cleared")
	}
}

func TestUsageBackfillAndPrune(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	old := time.Now().AddDate(0, 0, -UsageDays-5)
	s.AppendRun(Run{Switch: "a.sh", Name: "A", Action: "on", Source: "manual", OK: true, Time: time.Now()})
	// 模拟升级前：只有 runs.jsonl，没有 usage.json
	if err := os.Remove(filepath.Join(dir, usageFile)); err != nil {
		t.Fatal(err)
	}
	s2, _ := Open(dir)
	if s2.Usage().Days[time.Now().Format(DayLayout)].Total != 1 {
		t.Fatal("not backfilled from runs")
	}
	u := Usage{}
	u.add(Run{Switch: "old.sh", Name: "Old", Action: "on", Source: "manual", Time: old})
	u.add(Run{Switch: "new.sh", Name: "New", Action: "on", Source: "manual", Time: time.Now()})
	u.prune(time.Now())
	if len(u.Days) != 1 || u.Names["old.sh"] != "" || u.Names["new.sh"] != "New" {
		t.Fatalf("prune: %+v", u)
	}
}

func TestRunsFileCompactsWhileRunning(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	for i := 0; i < 2*MaxRecentRuns+5; i++ {
		if _, err := s.AppendRun(Run{Switch: "a.sh", Action: "status", Source: "manual", OK: true}); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, runsFile))
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines > MaxRecentRuns+10 {
		t.Fatalf("runs.jsonl has %d lines, want about %d", lines, MaxRecentRuns)
	}
	if got := len(s.Runs(Filter{})); got != MaxRecentRuns {
		t.Fatalf("in memory %d", got)
	}
}

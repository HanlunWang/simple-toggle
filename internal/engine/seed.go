package engine

import (
	"errors"
	"os"
	"path/filepath"
)

// Example 是一个预置脚本。
type Example struct {
	File           string
	Content        string
	ShownByDefault bool // false：安装后先在面板中隐藏，避免面板拥挤
}

// Seed 增量投放预置脚本：每个只投放一次（记在 store 里），用户删除后不会复活，
// 新版本新增的预置脚本能补装到已有安装。与 v1 的逻辑一致。
func (e *Engine) Seed(examples []Example) error {
	if err := os.MkdirAll(e.cfg.ScriptsDir, 0o755); err != nil {
		return err
	}
	var done []string
	for _, ex := range examples {
		if e.cfg.Store.Seeded(ex.File) {
			continue
		}
		path := filepath.Join(e.cfg.ScriptsDir, ex.File)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			if err := writeScript(path, ex.Content); err != nil {
				return err
			}
			if !ex.ShownByDefault {
				_ = e.cfg.Store.SetHidden(ex.File, true)
			}
		}
		done = append(done, ex.File)
	}
	if len(done) == 0 {
		return nil
	}
	return e.cfg.Store.MarkSeeded(done...)
}

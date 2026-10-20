package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
			// 检查之后才出现的同名文件（用户刚建的）同样不覆盖
			err := createScriptFile(path, ex.Content)
			switch {
			case err == nil:
				if !ex.ShownByDefault {
					_ = e.cfg.Store.SetHidden(ex.File, true)
				}
			case !errors.Is(err, os.ErrExist):
				return err
			}
		}
		done = append(done, ex.File)
	}
	if len(done) == 0 {
		return nil
	}
	return e.cfg.Store.MarkSeeded(done...)
}

// Upgrade 是一个可自动升级的预置脚本。
type Upgrade struct {
	File     string
	Content  string
	Previous []string // 以前发布过的版本：去掉末尾空白后的 SHA-256
}

// PresetFingerprint 是脚本内容的指纹（去掉末尾空白，不受编辑器补换行的影响）。
func PresetFingerprint(content string) string {
	sum := sha256.Sum256([]byte(strings.TrimRight(content, " \t\n")))
	return hex.EncodeToString(sum[:])
}

// UpgradePresets 把脚本目录里「与某个旧版预置完全相同」的脚本换成当前版本，
// 用户改过的不动。返回升级了的文件。
func (e *Engine) UpgradePresets(items []Upgrade) []string {
	var done []string
	for _, it := range items {
		path := filepath.Join(e.cfg.ScriptsDir, it.File)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fp := PresetFingerprint(string(data))
		if fp == PresetFingerprint(it.Content) || !slices.Contains(it.Previous, fp) {
			continue
		}
		if writeScript(path, it.Content) == nil {
			done = append(done, it.File)
		}
	}
	return done
}

# OpenToggle

macOS 菜单栏"自动化开关中心"MVP demo：把你自己写的脚本自动渲染成菜单栏里的受管开关（on/off + 状态灯），开=启动、关=停止、重启后记住状态。

> 完整规划见 `menubar-toggle-center-plan.md`（Downloads）。本 demo 覆盖 P1（MenuBarExtra 骨架）+ 简化版 P2（脚本目录扫描 + 注释头契约 + 进程管理 + 状态持久化）。

## 运行

```bash
swift run
```

菜单栏会出现一个 ⎘ 开关图标（SF Symbol `switch.2`），点开即是开关面板。终端 Ctrl+C 或面板里"退出"结束（退出时会清理所有 daemon 子进程）。

## 脚本契约

把脚本放进 `~/.config/open-toggle/switches/`（首次运行自动创建并写入两个示例），元数据写在注释头里：

```bash
#!/bin/bash
# <switch.name> My Switch      # 必填，显示名
# <switch.icon> 🚀             # 可选，emoji
# <switch.type> toggle         # toggle（默认）| daemon
```

两类形态：

- **`toggle`（命令式）**：app 调 `script on` / `script off` / `script status`；`status` 输出 `on` 或 `off`，app 每 5 秒轮询一次。
- **`daemon`（常驻进程）**：app 以 `script run` 启动并持有进程，关 = SIGTERM；状态 = 进程是否存活。脚本里用 `exec` 顶替 shell，让信号直达（见 `keep-awake.sh`）。

状态灯：🟢 on · ⚪ off · 🔴 error（daemon 意外退出 / 命令失败）· 🟠 unknown。

开关的期望状态存在 `UserDefaults`，app 重启时自动恢复（daemon 重新拉起；toggle 先查 `status`，已经是 on 就不重复执行）。

## 内置示例

| 开关 | 形态 | 做什么 |
|------|------|--------|
| ☕️ Keep Awake | daemon | `caffeinate -d` 保持亮屏 |
| 👁️ Show Hidden Files | toggle | Finder 显示/隐藏隐藏文件（会重启 Finder） |

两个都零权限。规划里的 Keyboard Jiggler（需 Accessibility 授权）留到原生 app bundle 阶段再加。

## 代码结构

```
Sources/OpenToggle/
├── OpenToggleApp.swift   # MenuBarExtra 入口 + 退出清理（LSUIElement 等价）
├── MenuView.swift        # 面板 UI：状态灯 + Toggle 每行一个开关
├── SwitchModel.swift     # 注释头契约解析
├── SwitchManager.swift   # 目录扫描 + 进程生命周期 + 5s 轮询 + UserDefaults 持久化
├── ScriptRunner.swift    # Process 封装（短命令 / daemon spawn）
└── ExampleScripts.swift  # 首次运行写入的示例脚本
```

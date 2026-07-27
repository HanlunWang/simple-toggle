# OpenToggle

macOS 菜单栏"自动化开关中心"MVP demo：把你自己写的脚本自动渲染成菜单栏里的受管开关（on/off + 状态灯），开=启动、关=停止、重启后记住状态。

> 完整规划见 `menubar-toggle-center-plan.md`（Downloads）。本 demo 覆盖 P1（MenuBarExtra 骨架）+ 简化版 P2（脚本目录扫描 + 注释头契约 + 进程管理 + 状态持久化）。

## 运行

```bash
swift run
```

菜单栏会出现一个 ⎘ 开关图标（SF Symbol `switch.2`），点开即是开关面板。终端 Ctrl+C 或面板里"退出"结束（退出时会清理所有 daemon 子进程）。

## 脚本契约

把脚本放进 `~/.config/open-toggle/switches/`（首次运行自动创建并写入两个示例），元数据写在注释头里（前 40 行内）：

```bash
#!/bin/bash
# <switch.name> My Switch      # 必填，显示名
# <switch.icon> 🚀             # 可选，emoji
# <switch.type> toggle         # toggle（默认）| daemon
```

两类形态：

- **`toggle`（命令式）**：app 调 `script on` / `script off` / `script status`；`status` 输出 `on` 或 `off`，app 每 5 秒轮询一次。
- **`daemon`（常驻进程）**：app 以 `script run` 启动并持有进程，关 = SIGTERM；状态 = 进程是否存活。脚本里用 `exec` 顶替 shell，让信号直达（见 `keep-awake.sh`）。daemon 自然退出（exit 0，如 `caffeinate -t` 到时）视为正常关闭，开关自动归位；非零退出亮红灯。

状态灯：🟢 on · ⚪ off · 🔴 error（daemon 意外退出 / 命令失败）· 🟠 unknown。

开关的期望状态存在 `UserDefaults`，app 重启时自动恢复（daemon 重新拉起；toggle 先查 `status`，已经是 on 就不重复执行）。

### 参数（`<switch.param>`）

开关行右侧的 ⌄ 可展开参数区，支持三种控件；参数值以**环境变量**注入脚本（`SWITCH_<KEY大写>`），开关开着时修改会立即重启/重跑生效：

```bash
# <switch.param> key=mode type=select label=模式 default=d options="仅保持屏幕=d|仅保持任务=is|屏幕与任务=dis"
# <switch.param> key=duration type=number label=时长(分钟) default=0 min=0 max=1440 hint="0 = 一直保持"
# <switch.param> key=note type=text label=备注 hint="随便写"
```

- `type=select`：下拉框，`options="标签=值|标签=值"`
- `type=number`：数字输入 + 步进器，可加 `min=` / `max=`
- `type=text`：自由填写框
- `presets="1小时=60|2小时=120"`（number/text 可选）：渲染成一排**快捷按钮**，点一下直接填值，与输入框并存、当前命中的高亮
- 值含空格用双引号包住；`hint=` 会显示为提示/tooltip

### 菜单栏图标（`<switch.menubar>`）

开关**开启时**可以在菜单栏额外露出存在感：

```bash
# <switch.menubar> mode=add icon=☕️ countdown=on
```

- `mode=add`：新增一个独立菜单栏图标（点击可直接关闭该开关）；`mode=replace`：临时替换 app 主图标
- `icon=`：emoji（`☕️`）或 SF Symbols（`sf:cup.and.saucer.fill`）
- `countdown=on`：图标旁显示倒计时（要求有一个 `key=duration` 的 number 参数，单位分钟，0=无限）

### 脚本管理窗口

面板底部「管理脚本」打开管理窗口：**侧边栏列出全部开关**（状态点 + 图标 + 类型），选中即可修改，右键可删除（移到废纸篓，开着的会先关掉）；左下角「＋」新建。

编辑器分两栏：左边表单配置元数据（名称、**图标选择器**——emoji 网格 / SF Symbols 网格 / 自定义，不用手打 `sf:xxx`、类型、参数、菜单栏行为），右边是**脚本文件视图**——上半部分是表单实时生成的契约头（只读，注释绿高亮），下半部分是深色等宽的**代码编辑区**，直接写脚本体；可用的 `$SWITCH_*` 环境变量以胶囊提示列在顶部。保存时契约头 + 脚本体合成完整文件写回；**正在运行的开关保存后立即用新内容热重启**。手写的脚本也能无损往返编辑（只接管契约行，其余内容原样保留）。

## 内置示例

| 开关 | 形态 | 做什么 |
|------|------|--------|
| ☕️ Keep Awake | daemon | `caffeinate`，三模式下拉（仅屏幕 / 仅任务 / 屏幕与任务）+ 时长参数；开启时菜单栏多一个 ☕️ + 倒计时 |
| 👁️ Show Hidden Files | toggle | Finder 显示/隐藏隐藏文件（会重启 Finder） |

> Keep Awake 的"仅保持任务/屏幕与任务"用了 `caffeinate -s`（防合盖睡眠），只在接电源时有效——这是 macOS 的限制。

两个都零权限。规划里的 Keyboard Jiggler（需 Accessibility 授权）留到原生 app bundle 阶段再加。

## 代码结构

```
Sources/OpenToggle/
├── OpenToggleApp.swift        # MenuBarExtra 入口（主图标可被 replace 模式覆盖）+ 编辑器窗口 + 退出清理
├── MenuView.swift             # 面板 UI：状态灯 + Toggle + 可展开的参数区（下拉/数字/填写框）
├── ManagerView.swift          # 脚本管理窗口：侧边栏（增删改）+ 表单 + 代码编辑区
├── IconPicker.swift           # 图标选择器（emoji / SF Symbols 网格）+ IconView 渲染
├── StatusBarController.swift  # 脚本声明的额外菜单栏图标（emoji/SF Symbol + 倒计时 + 点击关闭）
├── SwitchModel.swift          # 注释头契约解析（name/icon/type/param/menubar）
├── SwitchManager.swift        # 目录扫描 + 进程生命周期 + 参数注入/热重启 + 轮询 + 持久化
├── ScriptRunner.swift         # Process 封装（短命令 / daemon spawn，环境变量注入）
└── ExampleScripts.swift       # 首次运行写入的示例脚本
```

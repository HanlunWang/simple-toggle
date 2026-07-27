# OpenToggle

**把任何脚本变成 macOS 菜单栏里的受管开关。**

[English](README.md) | 简体中文

OpenToggle 把你的每个自动化脚本渲染成菜单栏中的一等公民开关——名称、状态灯、Toggle。开 = 启动自动化，关 = 停止，状态跨重启保持。脚本可用任何语言编写，声明几行元数据指令，进程生命周期、状态轮询、参数 UI 与持久化全部由 OpenToggle 接管。

## 为什么做这个

macOS 上的日常自动化——保持亮屏、防挂机、切代理、显示隐藏文件——传统上每个都要装一个独立小工具，菜单栏一堆图标。脚本转菜单栏的工具已经存在（SwiftBar、xbar、Hammerspoon），但它们的模型是「脚本 → 打印输出 → 显示」，都没有一等公民的**开关抽象**：受管的开/关语义、进程生命周期、状态持久化，每个插件都得手写一遍。

OpenToggle 的模型是「脚本 → 受管开关」。契约即产品。

|  | SwiftBar / xbar | Hammerspoon | **OpenToggle** |
|---|---|---|---|
| 心智模型 | 打印输出 → 显示 | Lua 自动化框架 | 脚本 → 受管开关 |
| 开/关语义 | 每个插件手写 | Lua 手写 | 内置 |
| 进程生命周期 | 手动（pid 文件、pkill） | 手动 | spawn / SIGTERM / 孤儿清理 |
| 状态持久化 | 手动 | 手动 | 自动，启动时恢复 |
| 参数 UI | 无 | 自己实现 | 声明式（下拉 / 数字 / 快捷按钮 / 文本） |

## 功能

- **两类开关形态** —— 命令式 `toggle`（on/off/status 命令）与常驻 `daemon`（启动并持有，SIGTERM 停止）
- **声明式参数** —— 下拉选择、带边界与快捷按钮的数字、自由文本；值以环境变量注入；修改即时热重启
- **菜单栏存在感** —— 开关开启期间可新增独立状态栏图标（或替换主图标），可选每秒刷新的倒计时
- **状态持久化** —— 期望状态持久保存并在启动时恢复：daemon 重新拉起，toggle 先查状态再按需执行
- **脚本管理 GUI** —— 侧边栏增删改、带字段级文档的元数据表单、图标选择器（Emoji / SF Symbols）、深色代码编辑器 + 自动生成的只读契约头；手写脚本可无损往返编辑
- **安全性** —— 退出时清理孤儿进程，daemon 自然结束（exit 0）自动复位，所有退出路径的未保存拦截，删除进废纸篓
- **多语言** —— 英文与简体中文，默认跟随系统，运行时可切换

## 环境要求

- macOS 14+
- Swift 工具链（当前从源码构建；app bundle 分发在路线图中）

## 快速开始

```bash
git clone git@github.com:HanlunWang/open-toggle.git
cd open-toggle
swift run
```

菜单栏出现开关图标。首次启动会安装一套开箱即用的预制开关（每个只安装一次，删除后不会复活）：

| 开关 | 形态 | 功能 | 默认 |
|---|---|---|---|
| ☕️ Keep Awake | daemon | `caffeinate`，模式下拉 + 1/2/4/8 小时快捷按钮，菜单栏倒计时 | 启用 |
| 👁️ Show Hidden Files | toggle | Finder 隐藏文件可见性 | 启用 |
| 🌙 Dark Mode | toggle | 系统深色外观（首次触发一次自动化权限弹窗） | 启用 |
| 🔇 Mute Audio | toggle | 系统输出静音 | 启用 |
| ▦ Hide Desktop Icons | toggle | 隐藏桌面图标，适合投屏/录屏 | 启用 |
| ⬒ Dock Auto-Hide | toggle | Dock 自动隐藏 | 启用 |
| 📶 Wi-Fi | toggle | Wi-Fi 开关（自动探测设备名） | 停用 |
| 🌐 Local HTTP Server | daemon | `python3 -m http.server`，端口 + 目录参数 | 停用 |
| 🛡 Web Proxy | toggle | 系统 HTTP/HTTPS 代理，服务 / 主机 / 端口参数 | 停用 |

停用的开关不出现在菜单栏，在脚本管理器侧边栏点眼睛图标即可启用。也可通过「管理脚本」新建，或直接把脚本放进目录后重新加载。

预制脚本以纯 `.sh` 文件形式放在 [`Sources/OpenToggle/Switches/`](Sources/OpenToggle/Switches/)（构建时作为 SwiftPM 资源打包）——可直接浏览作为契约示例，或复制一份到脚本目录作为起点。

## 脚本契约

一个开关 = `~/.config/open-toggle/switches/` 下的一个可执行脚本，元数据以 `# <switch.*>` 指令注释声明（前 40 行内）。

### 指令

| 指令 | 必填 | 说明 |
|---|---|---|
| `<switch.name>` | 是 | 显示名称 |
| `<switch.icon>` | 否 | Emoji，或 `sf:` 前缀的 SF Symbols 名（如 `sf:cup.and.saucer.fill`） |
| `<switch.type>` | 否 | `toggle`（默认）或 `daemon` |
| `<switch.param>` | 否 | 每行声明一个参数，见下文 |
| `<switch.menubar>` | 否 | 开启期间的菜单栏行为，见下文 |

### 调用协议

**`toggle`** —— 命令式，适合幂等操作：

```
<script> on       # 开启
<script> off      # 关闭
<script> status   # 向 stdout 输出 "on" 或 "off"；每 5 秒轮询
```

`status` 退出码非 0 时状态记为 *unknown*；`on`/`off` 非 0 记为 *error*。

**`daemon`** —— 前台常驻进程：

```
<script> run      # 由 app 启动并持有
```

关闭发送 SIGTERM；请使用 `exec` 确保信号直达。exit 0 视为自然结束并复位开关；非零退出码标记为 *error*。

### 参数

```bash
# <switch.param> key=mode type=select label=模式 default=d options="仅屏幕=d|屏幕与系统=dis"
# <switch.param> key=duration type=number label="时长(分钟)" default=0 min=0 max=1440 presets="不限=0|1小时=60|2小时=120"
# <switch.param> key=note type=text label=备注 hint="自由文本"
```

| 属性 | 适用类型 | 说明 |
|---|---|---|
| `key` | 全部 | 标识符；注入为 `SWITCH_<KEY>`（转大写，非字母数字 → `_`） |
| `type` | 全部 | `select` \| `number` \| `text` |
| `label` | 全部 | 面板中的显示标签 |
| `default` | 全部 | 初始值；`select` 缺省取第一个候选项的 value |
| `options` | select | `label=value` 对，以 `\|` 分隔 |
| `presets` | number、text | 快捷按钮，同 `label=value\|…` 格式；与输入框并存 |
| `min` / `max` | number | 取值边界（含端点） |
| `hint` | 全部 | 占位符（text）或悬停提示（其他） |

属性值含空格时以双引号包裹。每次调用均注入环境变量；开关开启时修改参数会以新环境重启。

### 菜单栏存在感

```bash
# <switch.menubar> mode=add icon=☕️ countdown=on
```

- `mode=add` —— 开启期间创建独立状态栏项，其菜单含一键关闭
- `mode=replace` —— 临时替换 app 主图标
- `icon` —— Emoji 或 `sf:<symbol-name>`
- `countdown=on` —— 显示剩余时间（每秒刷新）；依赖 key 为 `duration` 的 number 参数（分钟，`0` 表示不限时）

### 执行细节

- 保存时自动 `chmod 755`；无执行位的文件回退以 `/bin/sh` 执行
- 状态模型：`on` / `off` / `error` / `unknown`
- 期望状态存于 `UserDefaults`，启动时恢复：daemon 重新拉起；toggle 先查 `status`，仅在需要时执行 `on`
- app 退出时统一终止持有的 daemon 进程
- 保存正在运行的开关的脚本后，会以新内容立即重启

## 架构

```
Sources/OpenToggle/
├── OpenToggleApp.swift        # MenuBarExtra 入口、管理窗口、退出拦截
├── MenuView.swift             # 面板：状态灯、Toggle、可展开参数控件
├── ManagerView.swift          # 管理窗口：侧边栏增删改、元数据表单、代码编辑器
├── IconPicker.swift           # Emoji / SF Symbols 选择网格
├── StatusBarController.swift  # 脚本声明的状态栏项（图标 + 倒计时 + 菜单）
├── SwitchModel.swift          # 契约解析（指令、参数、菜单栏）
├── SwitchManager.swift        # 注册表、进程生命周期、轮询、持久化
├── ScriptRunner.swift         # Process 封装（命令 / daemon spawn、环境注入）
├── Localization.swift         # 运行时可切换的字符串表（en、zh-Hans）
├── ExampleScripts.swift       # 预制库清单（从 bundle 资源加载）
└── Switches/                  # 预制开关脚本（.sh），构建时打包为资源
```

## 路线图

- App bundle 打包、公证、Homebrew cask
- 依赖辅助功能权限的示例（CGEvent 键盘防挂机）
- Manifest 式契约（TOML）作为注释头的替代
- 定时调度、条件触发、开关依赖关系

## 许可证

MIT

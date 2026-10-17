# OpenToggle Homebrew cask
#
# 发布时把本文件复制到 tap 仓库 HanlunWang/homebrew-tap 的 Casks/opentoggle.rb，
# 并把 version 与 sha256 更新为 Scripts/release.sh 输出的值。
# GitHub 会把资源文件名里的空格换成点：mygo 生成的「OpenToggle 2.0.0.dmg」
# 在 Release 里叫「OpenToggle.2.0.0.dmg」（release.sh --upload 会打印实际名称）。
#
# 用户安装：
#   brew install --cask hanlunwang/tap/opentoggle
#
cask "opentoggle" do
  version "2.0.0"
  sha256 "REPLACE_WITH_RELEASE_SH_OUTPUT"

  url "https://github.com/HanlunWang/open-toggle/releases/download/v#{version}/OpenToggle.#{version}.dmg"
  name "OpenToggle"
  desc "Turn scripts into managed menu bar switches"
  homepage "https://github.com/HanlunWang/open-toggle"

  # app 会自己检查并安装更新（签名校验）；brew upgrade 不必替它升级
  auto_updates true
  depends_on macos: :sonoma

  app "OpenToggle.app"
  # CLI：opentoggle 命令就是 app 自己的二进制（与 0.5 相同的路径，已有的 MCP 配置不用改）
  binary "#{appdir}/OpenToggle.app/Contents/MacOS/OpenToggle", target: "opentoggle"

  zap trash: [
    "~/.config/open-toggle",
    "~/Library/Application Support/OpenToggle",
    "~/Library/Preferences/io.github.hanlunwang.OpenToggle.plist",
  ]

  caveats <<~EOS
    Switch scripts live in ~/.config/open-toggle/switches/
    Upgrading from 0.5 keeps your scripts, switch states and permissions.
  EOS
end

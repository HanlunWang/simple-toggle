# OpenToggle Homebrew cask 配方
#
# 使用方式：发布时把本文件复制到 tap 仓库 HanlunWang/homebrew-tap 的
# Casks/opentoggle.rb（tap 仓库不存在则先创建：gh repo create homebrew-tap --public），
# 并把 version 与 sha256 更新为 release.sh 输出的值。
#
# 用户安装：
#   brew tap hanlunwang/tap
#   brew install --cask opentoggle
#
cask "opentoggle" do
  version "0.5.0"
  sha256 "REPLACE_WITH_RELEASE_SH_OUTPUT"

  url "https://github.com/HanlunWang/open-toggle/releases/download/v#{version}/OpenToggle-#{version}.zip"
  name "OpenToggle"
  desc "Script-powered menu bar switches with managed lifecycle"
  homepage "https://github.com/HanlunWang/open-toggle"

  depends_on macos: ">= :sonoma"

  app "OpenToggle.app"
  # CLI：opentoggle 命令直接可用（同一二进制）
  binary "#{appdir}/OpenToggle.app/Contents/MacOS/OpenToggle", target: "opentoggle"

  zap trash: [
    "~/.config/open-toggle",
  ]

  caveats <<~EOS
    Switch scripts live in ~/.config/open-toggle/switches/
    Key-sending switches need a one-time Accessibility grant
    (System Settings → Privacy & Security → Accessibility).
  EOS
end

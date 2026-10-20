# SimpleToggle Homebrew cask
#
# 发布时把本文件复制到 tap 仓库 HanlunWang/homebrew-tap 的 Casks/simpletoggle.rb，
# 并把 version 与 sha256 更新为 Scripts/release.sh 输出的值。
# GitHub 会把资源文件名里的空格换成点：mygo 生成的「SimpleToggle 2.0.0.dmg」
# 在 Release 里叫「SimpleToggle.2.0.0.dmg」（release.sh --upload 会打印实际名称）。
#
# 用户安装：
#   brew install --cask hanlunwang/tap/simpletoggle
#
cask "simpletoggle" do
  version "2.0.0"
  sha256 "ed20cf75d1ebc939bd9fd40f35d5d599f386bceafe8c8cf1a43cc4b33fb30f14"

  url "https://github.com/HanlunWang/simple-toggle/releases/download/v#{version}/SimpleToggle.#{version}.dmg"
  name "SimpleToggle"
  desc "Turn scripts into managed menu bar switches"
  homepage "https://github.com/HanlunWang/simple-toggle"

  # app 会自己检查并安装更新（签名校验）；brew upgrade 不必替它升级
  auto_updates true
  depends_on macos: :sonoma

  app "SimpleToggle.app"
  # CLI：simpletoggle 命令就是 app 自己的二进制
  binary "#{appdir}/SimpleToggle.app/Contents/MacOS/SimpleToggle", target: "simpletoggle"

  zap trash: [
    "~/.config/simple-toggle",
    "~/Library/Application Support/SimpleToggle",
    "~/Library/Preferences/io.github.hanlunwang.SimpleToggle.plist",
  ]

  caveats <<~EOS
    Switch scripts live in ~/.config/simple-toggle/switches/
    Coming from OpenToggle 0.5? Your scripts are copied over on first launch;
    then `brew uninstall --cask opentoggle`. Grant Accessibility once more if a switch sends keys.
  EOS
end

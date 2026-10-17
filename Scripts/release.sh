#!/bin/bash
#
# SimpleToggle 2 发布：测试 → mygo build（通用二进制、Developer ID 签名、公证、装订、
# 自动更新文件）→ 验收产物 → 输出 cask 需要的 sha256。
#
#   ./Scripts/release.sh            只在本地构建与验收（不上传任何东西）
#   ./Scripts/release.sh --upload   另外上传到 GitHub 的草稿 Release（检查后再手动发布）
#
# 前置（一次性）：
#   1. Developer ID Application 证书（mygo.json 的 macos.signingIdentity）
#   2. xcrun notarytool store-credentials opentoggle-notary --apple-id <邮箱> --team-id 964MJX79NX
#   3. 更新签名私钥：mygo.json 的 updates.privateKey 指向的文件（或 MYGO_UPDATER_PRIVATE_KEY）。
#      私钥丢失后已安装的 app 将无法再收到更新——务必在密码管理器里留一份
#   4. 上传需要 gh 已登录
#
set -euo pipefail
cd "$(dirname "$0")/.."

UPLOAD=0
[ "${1:-}" = "--upload" ] && UPLOAD=1

VERSION="$(python3 -c 'import json;print(json.load(open("mygo.json"))["version"])')"
IDENTITY="$(python3 -c 'import json;print(json.load(open("mygo.json"))["macos"]["signingIdentity"])')"
PROFILE="$(python3 -c 'import json;print(json.load(open("mygo.json"))["macos"]["notarize"]["keychainProfile"])')"
KEY="$(python3 -c 'import json,os;print(os.path.expanduser(json.load(open("mygo.json"))["updates"].get("privateKey","")))')"

fail() { echo "✗ $*" >&2; exit 1; }

# ---- 前置检查：缺什么直说什么 ----
[ -z "$(git status --porcelain)" ] || fail "工作区有未提交的改动，先提交再发布"
grep -q "^## ${VERSION}\$" CHANGELOG.md || fail "CHANGELOG.md 缺少 ## ${VERSION} 一节（更新说明从这里读取）"
security find-identity -v -p codesigning | grep -qF "$IDENTITY" || fail "钥匙串里没有签名身份：$IDENTITY"
xcrun notarytool history --keychain-profile "$PROFILE" >/dev/null 2>&1 || fail "公证凭据 $PROFILE 不可用"
if [ -z "${MYGO_UPDATER_PRIVATE_KEY:-}" ]; then
  [ -f "$KEY" ] || fail "找不到更新签名私钥（updates.privateKey 或 MYGO_UPDATER_PRIVATE_KEY）"
fi
if [ "$UPLOAD" = 1 ]; then
  gh auth status >/dev/null 2>&1 || fail "gh 未登录"
  git rev-parse "v${VERSION}" >/dev/null 2>&1 && fail "标签 v${VERSION} 已存在"
fi

echo "==> SimpleToggle ${VERSION}"
echo "==> Tests"
go vet ./...
go test -count=1 ./...

echo "==> Build (universal, signed, notarized)"
rm -rf build
ARGS=(build -platform darwin/universal)
[ "$UPLOAD" = 1 ] && ARGS+=(-upload)
go tool mygo "${ARGS[@]}"

# ---- 验收 ----
APP="$(find build -maxdepth 3 -name 'SimpleToggle.app' -type d | head -1)"
DMG="$(find build -maxdepth 2 -name '*.dmg' | head -1)"
[ -n "$APP" ] && [ -n "$DMG" ] || fail "没有找到构建出的 app 或 dmg（build/）"
echo "==> Verify $APP"
[ -f "$APP/Contents/Resources/skills/simpletoggle/SKILL.md" ] || fail "skill 没有打进 app"
BIN="$APP/Contents/MacOS/SimpleToggle"
lipo -archs "$BIN" | grep -q arm64 && lipo -archs "$BIN" | grep -q x86_64 || fail "不是通用二进制：$(lipo -archs "$BIN")"
[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$APP/Contents/Info.plist")" = "io.github.hanlunwang.SimpleToggle" ] || fail "bundle id 不对"
codesign --verify --deep --strict "$APP"
spctl -a -vv --type execute "$APP" 2>&1 | grep -q "Notarized Developer ID" || fail "Gatekeeper 没有认出公证"
xcrun stapler validate "$DMG" >/dev/null || fail "dmg 没有装订公证票据"
"$BIN" version | grep -qF "$VERSION" || fail "二进制报告的版本不是 $VERSION"
find build -name "update-darwin-universal.json" | grep -q . || fail "没有生成自动更新清单（检查私钥配置）"

SHA="$(shasum -a 256 "$DMG" | awk '{print $1}')"
cat <<DONE

✓ SimpleToggle ${VERSION}
  dmg:     ${DMG}
  sha256:  ${SHA}
  (把 version 与 sha256 填进 Packaging/simpletoggle.rb，再复制到 tap 仓库的 Casks/simpletoggle.rb)

DONE
if [ "$UPLOAD" = 1 ]; then
  ASSET="$(gh release view "v${VERSION}" --json assets -q '.assets[].name' | grep '\.dmg$' | head -1)"
  echo "已上传到草稿 Release v${VERSION}（dmg 资源名：${ASSET}）。"
  echo "检查后在 GitHub 上发布：发布后已安装的 app 才会看到更新，cask 的下载地址也才可用。"
else
  echo "只在本地构建。确认无误后：./Scripts/release.sh --upload"
fi

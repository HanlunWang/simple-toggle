#!/bin/bash
#
# OpenToggle 发布流水线：
#   release 构建 → app bundle → Developer ID 签名（hardened runtime + timestamp）
#   → zip → notarytool 公证 → stapler 装订 → spctl 验收 → dist/ 产物 + cask sha256
#
# 前置（一次性）：
#   1. Apple Developer Program 会员生效
#   2. Xcode → Accounts → Manage Certificates → Developer ID Application
#   3. xcrun notarytool store-credentials "opentoggle-notary" \
#        --apple-id <AppleID邮箱> --team-id <TEAMID>
#
# 用法：./Scripts/release.sh
# 产物：dist/OpenToggle-<version>.zip（已公证装订，可直接挂 GitHub Release / brew cask）
#
set -euo pipefail
cd "$(dirname "$0")/.."

APP_NAME="OpenToggle"
BUNDLE_ID="io.github.hanlunwang.OpenToggle"
NOTARY_PROFILE="opentoggle-notary"
VERSION="$(grep -m1 'let appVersion' Sources/OpenToggle/ControlServer.swift | sed 's/.*"\(.*\)".*/\1/')"

# ---- 前置检查：缺什么直说什么 ----
IDENTITY="$(security find-identity -v -p codesigning 2>/dev/null \
  | sed -n 's/.*"\(Developer ID Application: [^"]*\)".*/\1/p' | head -1)"
if [ -z "$IDENTITY" ]; then
  echo "✗ 未找到 Developer ID Application 证书。"
  echo "  → Xcode → Settings → Accounts → Manage Certificates → ＋ → Developer ID Application"
  exit 1
fi
if ! xcrun notarytool history --keychain-profile "$NOTARY_PROFILE" >/dev/null 2>&1; then
  echo "✗ 公证凭据 \"$NOTARY_PROFILE\" 不可用。"
  echo "  → xcrun notarytool store-credentials \"$NOTARY_PROFILE\" --apple-id <邮箱> --team-id <TEAMID>"
  exit 1
fi
echo "==> Signing identity: $IDENTITY"

# ---- 构建与组装（与 package.sh 同构，但面向分发） ----
echo "==> Building release binary (v$VERSION)"
swift build -c release >/dev/null

STAGE_DIR="$(mktemp -d)"
STAGE="$STAGE_DIR/$APP_NAME.app"
mkdir -p "$STAGE/Contents/MacOS" "$STAGE/Contents/Resources"
cp ".build/release/$APP_NAME" "$STAGE/Contents/MacOS/$APP_NAME"
[ -d ".build/release/${APP_NAME}_${APP_NAME}.bundle" ] && \
  cp -R ".build/release/${APP_NAME}_${APP_NAME}.bundle" "$STAGE/Contents/Resources/"
[ -f "Assets/AppIcon.icns" ] && cp "Assets/AppIcon.icns" "$STAGE/Contents/Resources/AppIcon.icns"

cat > "$STAGE/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleIdentifier</key><string>$BUNDLE_ID</string>
    <key>CFBundleName</key><string>$APP_NAME</string>
    <key>CFBundleDisplayName</key><string>$APP_NAME</string>
    <key>CFBundleExecutable</key><string>$APP_NAME</string>
    <key>CFBundlePackageType</key><string>APPL</string>
    <key>CFBundleShortVersionString</key><string>$VERSION</string>
    <key>CFBundleVersion</key><string>$VERSION</string>
    <key>LSMinimumSystemVersion</key><string>14.0</string>
    <key>LSUIElement</key><true/>
    <key>CFBundleIconFile</key><string>AppIcon</string>
    <key>NSHighResolutionCapable</key><true/>
    <key>NSAppleEventsUsageDescription</key><string>Switch scripts may control system settings (e.g. appearance) via System Events.</string>
</dict>
</plist>
PLIST

# ---- 出厂断言：资源包必须在 bundle 里（v0.5.0 教训：编译机上的 dev
#      回退路径会掩盖资源缺失，只有别人的机器会崩） ----
if [ ! -d "$STAGE/Contents/Resources/${APP_NAME}_${APP_NAME}.bundle" ]; then
  echo "✗ 资源包未打进 app（Contents/Resources/${APP_NAME}_${APP_NAME}.bundle 缺失）"
  exit 1
fi

# ---- 签名（公证硬性要求：hardened runtime + 安全时间戳） ----
echo "==> Codesigning (hardened runtime)"
codesign --force --sign "$IDENTITY" --identifier "$BUNDLE_ID" \
  --options runtime --timestamp "$STAGE"
codesign --verify --strict --verbose=1 "$STAGE"

# ---- 打包与公证 ----
mkdir -p dist
ZIP="dist/$APP_NAME-$VERSION.zip"
rm -f "$ZIP"
ditto -c -k --keepParent "$STAGE" "$ZIP"

echo "==> Notarizing (this waits for Apple, typically 1-5 min)"
xcrun notarytool submit "$ZIP" --keychain-profile "$NOTARY_PROFILE" --wait

echo "==> Stapling ticket"
xcrun stapler staple "$STAGE"
# 重新打包装订后的 app（装订改变了 bundle 内容）
rm -f "$ZIP"
ditto -c -k --keepParent "$STAGE" "$ZIP"

# ---- 验收：以 Gatekeeper 的视角确认 ----
echo "==> Gatekeeper assessment"
spctl -a -vv --type execute "$STAGE"

SHA="$(shasum -a 256 "$ZIP" | awk '{print $1}')"
rm -rf "$STAGE_DIR"

cat <<DONE

✓ Release artifact ready: $ZIP
  version: $VERSION
  sha256:  $SHA   ← 填入 Packaging/opentoggle.rb 的 sha256 字段

Next:
  gh release create "v$VERSION" "$ZIP" --title "OpenToggle $VERSION" --notes "..."
  然后更新 brew tap（见 Packaging/opentoggle.rb 顶部说明）
DONE

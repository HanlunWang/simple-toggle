#!/bin/bash
#
# OpenToggle 打包：release 构建 → 标准 .app bundle → 稳定身份签名 → /Applications
#
# 为什么必须打包 + 真实签名：macOS 的权限数据库（TCC）按签名身份记录授权。
# `swift build` 的 ad-hoc 签名以内容哈希为身份，每次重新构建都会让
# 辅助功能授权失效。用稳定证书签名后，授权跨构建/更新存活。
#
# 签名身份选择顺序：
#   1. 环境变量 OPENTOGGLE_SIGN_IDENTITY（如 "Apple Development: you@x.com (TEAM)"）
#   2. 钥匙串中已有的第一个代码签名身份
#   3. 自动创建自签证书 "OpenToggle Development"（首次会弹 1-2 次钥匙串确认，
#      codesign 询问钥匙串访问时选「始终允许」）
#
set -euo pipefail
cd "$(dirname "$0")/.."

APP_NAME="OpenToggle"
BUNDLE_ID="io.github.hanlunwang.OpenToggle"
CERT_NAME="OpenToggle Development"
DEST="/Applications/$APP_NAME.app"
VERSION="$(grep -m1 'let appVersion' Sources/OpenToggle/ControlServer.swift | sed 's/.*"\(.*\)".*/\1/')"

echo "==> Building release binary (v$VERSION)"
swift build -c release >/dev/null
BIN=".build/release/$APP_NAME"
RES=".build/release/${APP_NAME}_${APP_NAME}.bundle"

echo "==> Assembling $APP_NAME.app"
STAGE_DIR="$(mktemp -d)"
STAGE="$STAGE_DIR/$APP_NAME.app"
mkdir -p "$STAGE/Contents/MacOS" "$STAGE/Contents/Resources"
cp "$BIN" "$STAGE/Contents/MacOS/$APP_NAME"
if [ -d "$RES" ]; then
  cp -R "$RES" "$STAGE/Contents/Resources/"
fi
if [ -f "Assets/AppIcon.icns" ]; then
  cp "Assets/AppIcon.icns" "$STAGE/Contents/Resources/AppIcon.icns"
fi

cat > "$STAGE/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleIdentifier</key>
    <string>$BUNDLE_ID</string>
    <key>CFBundleName</key>
    <string>$APP_NAME</string>
    <key>CFBundleDisplayName</key>
    <string>$APP_NAME</string>
    <key>CFBundleExecutable</key>
    <string>$APP_NAME</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>$VERSION</string>
    <key>CFBundleVersion</key>
    <string>$VERSION</string>
    <key>LSMinimumSystemVersion</key>
    <string>14.0</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>LSUIElement</key>
    <true/>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>NSAppleEventsUsageDescription</key>
    <string>Switch scripts may control system settings (e.g. appearance) via System Events.</string>
</dict>
</plist>
PLIST

# ---- 签名身份（优先 Developer ID：与分发版同身份，辅助功能授权不漂移） ----
find_identity() {
  local all
  all=$(security find-identity -v -p codesigning 2>/dev/null | sed -n 's/.*"\(.*\)".*/\1/p')
  echo "$all" | grep -m1 "^Developer ID Application" || echo "$all" | head -1
}

IDENTITY="${OPENTOGGLE_SIGN_IDENTITY:-$(find_identity)}"

if [ -z "$IDENTITY" ]; then
  echo "==> No code-signing identity found; creating self-signed certificate \"$CERT_NAME\""
  CERT_TMP="$(mktemp -d)"
  cat > "$CERT_TMP/ext.cnf" <<EOF
[req]
distinguished_name = dn
x509_extensions = v3
prompt = no
[dn]
CN = $CERT_NAME
[v3]
keyUsage = critical,digitalSignature
extendedKeyUsage = critical,codeSigning
basicConstraints = critical,CA:false
EOF
  # 用系统 /usr/bin/openssl（LibreSSL）：其 pkcs12 默认算法与 `security import` 兼容；
  # Homebrew 的 OpenSSL 3 默认 AES/SHA256 MAC 会导致 "MAC verification failed"
  /usr/bin/openssl req -x509 -newkey rsa:2048 -days 3650 -nodes \
    -keyout "$CERT_TMP/key.pem" -out "$CERT_TMP/cert.pem" \
    -config "$CERT_TMP/ext.cnf" 2>/dev/null
  /usr/bin/openssl pkcs12 -export -inkey "$CERT_TMP/key.pem" -in "$CERT_TMP/cert.pem" \
    -out "$CERT_TMP/cert.p12" -passout pass:opentoggle 2>/dev/null
  security import "$CERT_TMP/cert.p12" \
    -k "$HOME/Library/Keychains/login.keychain-db" \
    -P opentoggle -T /usr/bin/codesign >/dev/null
  # 信任该证书用于代码签名（可能弹一次系统确认）
  security add-trusted-cert -p codeSign \
    -k "$HOME/Library/Keychains/login.keychain-db" "$CERT_TMP/cert.pem" || true
  rm -rf "$CERT_TMP"
  IDENTITY="$CERT_NAME"
fi

echo "==> Signing with identity: $IDENTITY"
codesign --force --sign "$IDENTITY" --identifier "$BUNDLE_ID" "$STAGE"
codesign --verify --verbose=1 "$STAGE"

echo "==> Installing to $DEST"
if pgrep -x "$APP_NAME" >/dev/null; then
  pkill -x "$APP_NAME" || true
  sleep 1
fi
rm -rf "$DEST"
ditto "$STAGE" "$DEST"
rm -rf "$STAGE_DIR"

# CLI 软链跟随安装位置
if [ -d "$HOME/.local/bin" ]; then
  ln -sf "$DEST/Contents/MacOS/$APP_NAME" "$HOME/.local/bin/opentoggle"
  echo "==> CLI symlink updated: ~/.local/bin/opentoggle → $DEST"
fi

echo "==> Launching"
open "$DEST"

cat <<NOTE

Done. $APP_NAME $VERSION installed at $DEST

First run after (re)signing:
  - Grant Accessibility once: the panel shows an orange banner with a
    shortcut button, or run \`opentoggle doctor\` to check.
  - The grant now SURVIVES rebuilds — repackage with this script anytime.
NOTE

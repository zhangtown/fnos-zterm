#!/usr/bin/env bash
# 打包 zterm.fpk（飞牛无端口内嵌应用）。
#
#   bash deploy/fnos-app/pack.sh            # 图标 → 前端（有 node_modules 就构建）→ 交叉编译 → 打包
#   SKIP_UI=1 bash deploy/fnos-app/pack.sh  # 跳过前端构建，用现有的 internal/webui/dist
#   BUMP=1 bash deploy/fnos-app/pack.sh     # 先把 manifest 的版本号 patch +1 再打包
#
# 升级注意（实测）：`appcenter-cli install-fpk` 对**已安装**的同名应用是空操作，
# 即使版本号更高也只打印 "Application [zterm] is installed."，文件一个字节都不换。
# 真正替换必须 `install-local -d <解包目录> -v <卷>`，见 deploy/fnos-app/install.sh。
#
# 前置：fnpack 在 .toolchain/fnpack/（Windows 为 fnpack.exe）
# Output: deploy/fnos-app/zterm.fpk
set -e
cd "$(dirname "$0")/../.."
APP=deploy/fnos-app/zterm
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*) FNPACK=${FNPACK:-.toolchain/fnpack/fnpack.exe} ;;
  *) FNPACK=${FNPACK:-.toolchain/fnpack/fnpack} ;;
esac

# 0) 可选：递增 patch 版本号
if [ -n "$BUMP" ]; then
  cur="$(sed -n 's/^version=//p' "$APP/manifest" | head -n1)"
  new="$(printf '%s' "$cur" | awk -F. '{printf "%s.%s.%d", $1, $2, $3+1}')"
  tmp="$(mktemp)"
  sed "s/^version=.*/version=$new/" "$APP/manifest" > "$tmp" && mv "$tmp" "$APP/manifest"
  echo "== 版本 $cur → $new"
fi
VERSION="$(sed -n 's/^version=//p' "$APP/manifest" | head -n1)"

# 定位 go（新开的 shell 里可能不在 PATH 上）
if ! command -v go >/dev/null 2>&1; then
  for c in /c/Users/*/go-sdk/go/bin/go.exe "$USERPROFILE/go-sdk/go/bin/go.exe" /usr/local/go/bin/go /usr/lib/go/bin/go; do
    [ -x "$c" ] && export PATH="$(dirname "$c"):$PATH" && break
  done
fi
command -v go >/dev/null 2>&1 || { echo "找不到 go，请先装 Go 或把它加到 PATH"; exit 1; }

# 1) 图标（必须先于编译：成品包与界面里的标识都来自这一步）
echo "== 生成图标"
go run ./tools/mkicon -out "$APP"

# 2) 前端（vite 直接输出到 internal/webui/dist，由 go:embed 打进二进制）
if [ -z "$SKIP_UI" ]; then
  if [ -d webui/node_modules ]; then
    echo "== 构建前端 (vite)"
    ( cd webui && npm run build )
  else
    echo "== 跳过前端构建（webui/node_modules 不存在；先 cd webui && npm install）"
  fi
fi
[ -f internal/webui/dist/index.html ] || { echo "internal/webui/dist 里没有 index.html，无法打包"; exit 1; }
echo "== 前端产物: $(ls internal/webui/dist | tr '\n' ' ')"

# 3) 后端（linux/amd64 静态二进制，前端已内嵌）
echo "== 编译 zterm (linux/amd64)"
mkdir -p "$APP/app/bin"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags="-s -w -X main.version=$VERSION" \
  -o "$APP/app/bin/zterm" ./cmd/zterm
chmod +x "$APP/app/bin/zterm"
ls -la "$APP/app/bin/zterm"

# 4) 打包（确保没有 wizard/：非交互安装遇到向导变量会先卸载再失败）
echo "== fnpack build"
if [ ! -x "$FNPACK" ]; then
  if command -v fnpack >/dev/null 2>&1; then FNPACK=fnpack; else echo "缺少 $FNPACK（见脚本头注释）"; exit 1; fi
fi
"$FNPACK" build --directory "$APP"
mv -f ./*.fpk deploy/fnos-app/ 2>/dev/null || true
echo "== 包内容抽查"
tar tzf deploy/fnos-app/zterm.fpk | head -20
echo "== 已生成 deploy/fnos-app/zterm.fpk（$VERSION）"
echo "   安装/升级：bash deploy/fnos-app/install.sh --no-bump"

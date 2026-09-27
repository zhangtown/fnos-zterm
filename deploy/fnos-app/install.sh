#!/usr/bin/env bash
# 一键部署/升级 zterm 到飞牛 NAS（打包 → 上传 → 安装 → 自检）
#
#   bash deploy/fnos-app/install.sh              # 版本号 patch+1 后打包安装
#   bash deploy/fnos-app/install.sh --no-bump    # 用当前版本号（首次安装）
#
# NAS 连接信息放 deploy/fnos-app/nas.env（不入仓库，见 nas.env.example）：
#   NAS=user@host   NAS_PORT=22   NAS_KEY=~/.ssh/id_ed25519   NAS_VOL=1
#   ZTERM_SOCK=/var/apps/zterm/target/app.sock   # 可选，自检用的 socket 路径
#
# 前置：本机私钥可免密登录 NAS；NAS 上 sudo 免密放行 /usr/local/bin/appcenter-cli。
# 为什么不用 `install-fpk`：对已安装的同名应用它是空操作（只打印 "Application [zterm] is installed."），
# 必须 `install-local -d <解包目录> -v <卷>`（内部流程：停止 → 卸载 → 安装 → 启动）。
set -e
cd "$(dirname "$0")/../.."

if [ -f deploy/fnos-app/nas.env ]; then
  # shellcheck disable=SC1091
  . deploy/fnos-app/nas.env
fi
NAS="${NAS:-}"
NAS_PORT="${NAS_PORT:-22}"
NAS_KEY="${NAS_KEY:-$HOME/.ssh/id_ed25519}"
NAS_VOL="${NAS_VOL:-1}"
ZTERM_SOCK="${ZTERM_SOCK:-/var/apps/zterm/target/app.sock}"
if [ -z "$NAS" ]; then
  echo "缺少 NAS 连接信息：把 deploy/fnos-app/nas.env.example 复制成 nas.env 并填自己的 NAS" >&2
  exit 2
fi
SSH=(ssh -i "$NAS_KEY" -p "$NAS_PORT" -o BatchMode=yes -o ConnectTimeout=8 "$NAS")
SCP=(scp -i "$NAS_KEY" -P "$NAS_PORT" -o BatchMode=yes)

BUMP=1
[ "$1" = "--no-bump" ] && BUMP=

if [ -n "$BUMP" ]; then
  BUMP=1 bash deploy/fnos-app/pack.sh
else
  bash deploy/fnos-app/pack.sh
fi
VERSION="$(sed -n 's/^version=//p' deploy/fnos-app/zterm/manifest | head -n1)"
FPK=deploy/fnos-app/zterm.fpk

echo
echo "== 上传 zterm $VERSION 到 $NAS"
"${SCP[@]}" "$FPK" "$NAS:/tmp/zterm-$VERSION.fpk" >/dev/null
echo "   已上传 /tmp/zterm-$VERSION.fpk"

echo "== 远端安装（install-local -v $NAS_VOL）"
"${SSH[@]}" "set -e
rm -rf ~/zterm-pkg && mkdir -p ~/zterm-pkg
tar xzf /tmp/zterm-$VERSION.fpk -C ~/zterm-pkg
sudo -n /usr/local/bin/appcenter-cli install-local -d \"\$HOME/zterm-pkg\" -v $NAS_VOL 2>&1 \
  | tr '\r' '\n' | grep -vE '^[\\\\/|.-]* ?(Verifying|installing|uninstalling|starting|stopping)' | tail -6
sleep 3"

echo "== 自检（unix socket，直连后端）"
"${SSH[@]}" "SOCK='$ZTERM_SOCK'
if [ ! -S \"\$SOCK\" ]; then
  for c in /var/apps/zterm/target/app.sock /var/apps/zterm/*/app.sock; do
    [ -S \"\$c\" ] && SOCK=\"\$c\" && break
  done
fi
AH=\"-H X-Trim-Isadmin:true -H X-Trim-Userid:1000 -H X-Trim-Username:selfcheck\"
echo \"  socket:   \$SOCK\"
echo -n '  已装版本: '; sudo -n /usr/local/bin/appcenter-cli list 2>&1 | awk -F'│' '/[^a-z]zterm[^a-z]/{gsub(/ /,\"\",\$4); print \$4; exit}'
echo -n '  health:   '; curl -s --unix-socket \"\$SOCK\" http://localhost/api/health; echo
echo -n '  会话列表: '; curl -s \$AH --unix-socket \"\$SOCK\" http://localhost/api/sessions; echo
echo '  静态资源:'
for u in \$(curl -s --unix-socket \"\$SOCK\" http://localhost/ | grep -oE '(src|href)=\"[^\"]+\"' | sed -E 's/.*=\"([^\"]+)\"/\1/'); do
  case \"\$u\" in http*|'//'*) continue;; esac
  curl -s --unix-socket \"\$SOCK\" -o /dev/null -w \"    \$u -> %{http_code} %{content_type} %{size_download}B\n\" \"http://localhost/\${u#./}\"
done
echo -n '  会话信息条数: '; curl -s \$AH --unix-socket \"\$SOCK\" http://localhost/api/system | tr ',' '\n' | grep -c . || true
echo -n '  无身份头拦截: '; curl -s -o /dev/null -w '%{http_code}\n' --unix-socket \"\$SOCK\" http://localhost/api/sessions"
echo
echo "== 完成：飞牛桌面打开「zterm」（仅管理员可见）"
echo "   数据目录: /usr/local/apps/@appdata/zterm/data   （主机簿、设置；升级/卸载都不动）"
echo "   日志:     /usr/local/apps/@appdata/zterm/lifecycle.log"

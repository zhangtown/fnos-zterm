# zterm —— 飞牛 NAS 上的终端应用

给飞牛 fnOS 写的一个终端：**本地 shell 与 SSH 都在服务端跑**，浏览器只是显示器。
刷新页面、切到别的应用、关掉标签页、手机锁屏再回来，里面的命令都还在跑，滚动历史也整屏恢复。
走飞牛**统一网关**（`/app/zterm/`），**不占用任何端口**，**只有管理员看得见**。

界面：会话侧栏 + xterm.js 终端，窗口尺寸联动（vim/htop/less 会跟着变）、复制粘贴、断线自动重连、
主机簿、深浅主题、移动端按键条（Esc/Tab/方向键/回车）。

---

## 一、装到 NAS 上

```bash
# 本机（Windows 上 Git Bash 也行；go 不在 PATH 也没关系，pack.sh 会自己找）
bash deploy/fnos-app/pack.sh                  # 图标 → 前端 → linux/amd64 交叉编译 → zterm.fpk
bash deploy/fnos-app/install.sh --no-bump     # 打包 + 上传 + 安装 + 自检（首次安装）
bash deploy/fnos-app/install.sh               # 升级：版本号 patch +1 再装
```

NAS 连接信息写在 `deploy/fnos-app/nas.env`（不入库，模板见 `nas.env.example`）：

```
NAS=user@your-nas:22 的账号@主机
NAS_PORT=22
NAS_KEY=$HOME/.ssh/id_ed25519
NAS_VOL=1          # 装到哪个存储卷
```

装完飞牛桌面左侧就有「zterm」，点开即用（**仅管理员可见**：普通账号看不到图标，
直接访问 `/app/zterm/` 也会被挡）。

### 两个必须记住的坑

1. **`appcenter-cli install-fpk` 对已安装的应用是空操作。**
   即使包里版本号更高，它也只会打印 `Application [zterm] is installed.`，一个字节都不换。
   升级必须走 `install-local -d <解包目录> -v <卷>`（内部流程：停止 → 卸载 → 安装 → 启动），`install.sh` 已经这么做了。
2. **包里不能有 `wizard/` 目录。**
   非交互安装遇到向导变量会先卸载再失败，等于把装好的应用搞掉。本项目的包没有向导项。

---

## 二、它是怎么跑起来的

```
浏览器 ──https──> 飞牛网关 :443 ──unix socket──> zterm 进程 ──pty──> /bin/bash（root）
                                             └─ ssh ──> 远程主机
```

- 后端只监听 `${TRIM_APPDEST}/app.sock`（实测为 `/usr/local/apps/@appcenter/zterm/app.sock`，
  `/var/apps/zterm/target/app.sock` 是指向它的软链）。
- 生命周期脚本 `deploy/fnos-app/zterm/cmd/main` 启动后**等 socket 文件出现再退出**，
  然后 `chmod 666`：网关换用户连 socket，权限不够的表现就是"点开一片白"。
- 前端 `<base href="__ZTERM_BASE__">` 由服务端按请求路径注入，所以挂在 `/app/zterm/` 下资源不会 404。
  相对路径构建在 `webui/vite.config.js` 里（`base: "./"`）。
- 界面资源**内嵌进二进制**（`internal/webui` 的 `go:embed all:dist`），成品包里没有"界面目录没跟过去"这种问题。

### 目录

```
cmd/zterm/main.go            入口：-data / -socket / -addr / -prefix / -version
internal/auth/               飞牛身份识别（管理员判定、UID 归一）
internal/session/            PTY 会话管理：创建/保活/滚动历史（环形缓冲）/重放/回收
internal/profiles/           主机簿与设置落库（按账号分目录）
internal/server/             HTTP + WebSocket 路由、静态资源、网关前缀剥离
internal/webui/              前端嵌入（dist 由 vite 输出到这里）
webui/                       前端源码：index.html / src/{main,api,theme}.js / style.css
tools/mkicon/                Go 画图标（浅底玻璃风），输出 ICON.PNG / ICON_256.PNG / app/ui/images/*
deploy/fnos-app/zterm/       飞牛应用包本体：manifest、cmd/*、config/*、app/ui/*
deploy/fnos-app/pack.sh      打包
deploy/fnos-app/install.sh   一键部署 + 自检
deploy/fnos-app/e2e-ws.py    端到端自检（直连 socket，不用浏览器）
```

---

## 三、接口

Web 界面用的 REST + WebSocket；`X-Trim-*` 之类的身份头由网关注入，代码里有兜底。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/health` | 免鉴权：版本、pid、会话数、管理员策略 |
| GET | `/api/whoami` | 当前身份（**核实网关到底发了哪些头就用它**） |
| GET/POST | `/api/sessions` | 列会话 / 建会话（`kind: shell` 或 `ssh`，可带 `profileId`） |
| PATCH/DELETE | `/api/sessions/{id}` | 重命名 / 删除 |
| GET | `/api/sessions/{id}/ws` | WebSocket：二进制帧=PTY 输出；文本帧=控制/退出事件 |
| GET/POST | `/api/profiles` | 主机簿 |
| GET/POST | `/api/settings` | 界面设置 |
| GET | `/api/system` | 主机名、负载、内存、磁盘等侧栏信息 |

WebSocket 协议（客户端 → 服务端，文本帧）：

```json
{"type":"input","data":"ls\r"}      // 直接写 PTY（\r 是回车）
{"type":"resize","cols":120,"rows":40}
{"type":"ping"}
```

服务端 → 客户端：**二进制帧**是 PTY 原始输出（含 ANSI），文本帧只有 `{"type":"exit","code":0}` 和 `{"type":"pong"}`。
连上就会先收到整块滚动历史，所以刷新/重连看到的是完整画面而不是白屏。服务端每 30s 发自心跳 ping，
客户端 90s 没动静会被服务端断开（会话本身不受影响，重连即可）。

---

## 四、数据、升级与卸载

| 内容 | 位置 |
| --- | --- |
| 主机簿、界面设置 | `/usr/local/apps/@appdata/zterm/data/users/<uid>/` |
| 应用日志 | `/usr/local/apps/@appdata/zterm/lifecycle.log`、`data/app.log` |
| 程序本体 | `/usr/local/apps/@appcenter/zterm/`（`/var/apps/zterm/target/` 是软链） |

- 数据按**飞牛账号**分目录，升级覆盖不到；`uninstall_callback` 也**不删**数据，想彻底清干净自己删 `data/`。
- 会话（PTY 与滚动历史）**只在内存里**：应用停止/重启会一起结束。所谓"保活"是指**浏览器侧**断线
  （刷新、切走、锁屏、换网络）不影响它，不跨应用重启。
- 主机簿**不存密码**。密码/密钥交互交给 ssh 自己在终端里问；要用密钥就走终端里 `ssh` 的默认行为
  （进程以 root 跑，也就是用 `/root/.ssh/` 下的密钥）。

---

## 五、安全边界（请认真读一遍）

- **应用以 root 跑**（`install_type=root`），所以本地 shell 会话就是 **NAS 上的 root shell**，
  能读写任何文件。这是"管理员终端"的常规形态，但你要清楚这一点。
- **可见性只靠飞牛自己的管理员判定**：普通账号看不到图标，访问 `/app/zterm/` 也会被挡。
- `ZTERM_ADMIN_MODE` 控制后端校验强度，写在 `cmd/main` 里：
  - `soft`（默认）：请求里带管理员标记就必须是管理员；**没带标记则放行**。
    网关若换了头名不会把应用锁死，代价是理论上绕开网关直连 socket 就没有管理员校验。
  - `strict`：必须显式拿到管理员标记，否则 403。**核实过头名之后再切**：
    管理员登录后打开 `/app/zterm/api/whoami`，看回显里到底有哪些 `X-Trim-*` 头，
    再把 `cmd/main` 里 `ZTERM_ADMIN_MODE` 的默认值改成 `strict` 重新打包。
    启动日志里也会留一行提示，列出当前试过的头名。
  - `off`：不校验（只在自己完全可控的环境里用）。
- 同账号最多 8 个并发会话（`internal/session` 里的 `MaxPerUID`），每个会话滚动历史 512KB（`NewManager(scrollbackKB)`）。
- 只要 PTY 里还有东西活着（shell 没退出），会话就一直保留——**不会因为你关掉浏览器就被回收**；
  命令退出后它的记录与最后一段输出再留 10 分钟（`Retain`）供回看，然后清掉。

---

## 六、自检与排错

```bash
# 端到端（不用浏览器）：建会话 → WS 连接 → 输入回显 → resize → 断开重连拿回历史 → 删会话
ssh -p 2288 -i ~/.ssh/yourkey user@your-nas 'python3 -' < deploy/fnos-app/e2e-ws.py

# 手动看后端
ssh ... 'curl -s --unix-socket /var/apps/zterm/target/app.sock http://localhost/api/health'
```

常见现象：

| 现象 | 先看哪里 |
| --- | --- |
| 桌面点开一片空白 | 静态资源是否 404（前缀/<base>）；`app.sock` 权限是否 666；后端是否在跑 |
| 应用中心显示"启动失败" | `/usr/local/apps/@appdata/zterm/lifecycle.log`：多半是 socket 没建出来或二进制没有执行位 |
| 装完版本号没变 | 你用了 `install-fpk`，它对已安装应用是空操作，改用 `install.sh` |
| 桌面看不到图标 | 只有管理员可见；普通账号看不到是正常的。管理员登录后刷新一下桌面 |
| 会话一刷新就没了 | 后端在重启（`ctl_stop=true` 时升级/停止会结束全部会话）；看 lifecyle 日志的启动时间 |
| 粘贴不了 / 移动端没方向键 | 移动端用底部按键条；桌面端用 Ctrl+Shift+V 或右键粘贴 |

---

## 七、本地开发

```bash
# 后端（Windows 上直接跑，PTY 走 conpty/其它实现）
go run ./cmd/zterm -addr 127.0.0.1:7791 -data /tmp/zterm-dev -ui webui

# 前端热更新（vite 代理到上面的后端）
cd webui && npm install && npm run dev
```

改完前端别忘 `npm run build`（产物进 `internal/webui/dist`，会被打进二进制）；
`pack.sh` 在 `webui/node_modules` 存在时会自动构建，`SKIP_UI=1` 可跳过。

打包版本号、图标、`app/ui/images` 都由 `pack.sh` 重新生成，不需要手工维护。

# zterm

**飞牛 fnOS 上的终端应用。** 本地 shell 和 SSH 都在 NAS 上跑，浏览器只是显示器——
**刷新页面、切到别的应用、关掉标签页、手机锁屏再回来，里面跑着的命令都不会断**，
滚动历史也整屏恢复。

走飞牛**统一网关**（`/app/zterm/`），**不占用任何端口**，**只有管理员看得见**。

---

## 它能干什么

| 能力 | 说明 |
| --- | --- |
| **本地终端** | 在 NAS 上开 shell（默认 `bash`，可选 `sh`，工作目录可指定）。以 root 运行，就是 NAS 的 root shell。 |
| **会话保活** | 会话活在服务端。关掉浏览器、换网络、手机锁屏都不会杀它；回来点一下接着用。 |
| **滚动历史** | 服务端保留每个会话 512KB 输出，重连时**整屏恢复**，不是从空白开始。 |
| **多会话** | 左侧列表管理多个会话：新建、切换、重命名、删除；每账号最多 8 个并发。 |
| **远程 SSH** | 内置主机簿，存用户名/端口/附加参数，一键拨过去。**不存密码和私钥**，交互交给 `ssh` 自己。 |
| **尺寸联动** | 窗口/侧栏变化自动 `SIGWINCH`，`vim`、`htop`、`less`、`top` 会跟着窗口调整。 |
| **复制粘贴** | 选中即复制或 `Ctrl+Shift+C`；粘贴走确认框，多行粘贴可"粘完即回车"。 |
| **好用的终端** | xterm.js：真彩色、URL 可点、5000 行本地回滚、字号调节、光标闪烁开关。 |
| **深浅主题** | 跟随应用设置切换亮/暗，深色是默认（终端区为深色设计）。 |
| **手机可用** | 窄屏出现底部按键条：`Esc` `Tab` `Ctrl+C` `Ctrl+D` `↑` `End` 和 `|` `-` `~` `/`，手机上打符号不用来回切键盘。 |
| **零端口** | 只监听 `app.sock`，由飞牛网关转发，不用记端口、不用做端口映射。 |
| **按账号隔离** | 主机簿和设置按飞牛账号分目录存放（用网关注入的 `X-Trim-Userid`）。 |

## 它不能干什么

- **不跨应用重启**：会话是内存态，升级/停止/重启 zterm 会结束全部会话（要跨重启保活请在里面跑 `tmux`/`screen`）。
- **不是多用户终端**：应用以 root 跑，能进来的人就是管理员，看不到"别的用户的会话"这种概念。
- **不保存密码**：不做凭证托管，SSH 密码/密钥交互都在终端里进行。

---

## 快速开始

### 方式 A：从源码打包安装（推荐，一条命令）

前置：**Go ≥ 1.23**（`go.mod` 声明，本项目只依赖 `creack/pty` 与 `gorilla/websocket`）、
**Node ≥ 20.19**（vite 7 要求；已有 `internal/webui/dist` 时可用 `SKIP_UI=1` 跳过前端构建）、
**[fnpack](https://static2.fnnas.com/fnpack/)**（飞牛官方打包工具，放到 `.toolchain/fnpack/`）。
`zterm` 本体是**纯 Go + 前端静态文件**，无运行时依赖，NAS 上什么都不用装。

```bash
# 1) 下载 fnpack 到 .toolchain/fnpack/
#    Windows: https://static2.fnnas.com/fnpack/fnpack-1.2.3-windows-amd64 -> .toolchain/fnpack/fnpack.exe
#    Linux:   https://static2.fnnas.com/fnpack/fnpack-1.2.3-linux-amd64   -> .toolchain/fnpack/fnpack

# 2) 告诉脚本你的 NAS（该文件已被 .gitignore 忽略）
cp deploy/fnos-app/nas.env.example deploy/fnos-app/nas.env
vi deploy/fnos-app/nas.env        # NAS=user@host  NAS_PORT=22  NAS_KEY=~/.ssh/id_ed25519  NAS_VOL=1
                                  # 要求：该账号能用密钥免密 ssh 登录、且有 sudo 权限

# 3) 打包 + 上传 + 安装 + 自检（首次安装）
bash deploy/fnos-app/install.sh --no-bump

# 4) 之后升级：版本号 patch +1 再装
bash deploy/fnos-app/install.sh
```

装完飞牛桌面就会出现「zterm」图标。想只打包不装：`bash deploy/fnos-app/pack.sh`，
产物在 `deploy/fnos-app/zterm.fpk`（可以直接拿去应用中心手动装）。

### 方式 B：手动安装现成的 fpk

```bash
scp deploy/fnos-app/zterm.fpk user@your-nas:/tmp/
ssh user@your-nas
mkdir -p ~/zterm-pkg && tar xzf /tmp/zterm.fpk -C ~/zterm-pkg
sudo /usr/local/bin/appcenter-cli install-local -d ~/zterm-pkg -v 1   # 1 = 装到哪个存储卷
```

> ⚠️ **两个必须知道的坑**（都是实测踩出来的）
> 1. **`appcenter-cli install-fpk` 对已安装的同名应用是空操作**——哪怕包里版本号更高，
>    它也只打印 `Application [zterm] is installed.`，一个字节都不换。升级必须用 `install-local`。
> 2. **包里不能有 `wizard/` 目录**——非交互安装遇到向导变量会*先卸载再失败*，等于把装好的应用搞掉。

---

## 使用步骤

### 1. 打开

用**管理员账号**登录飞牛桌面 → 左侧点「zterm」。普通账号既看不到图标，直接访问 `/app/zterm/` 也会被挡。

### 2. 开第一个会话

点左上 **`＋`**（或 `Alt+Shift+T`）→ 填：

| 字段 | 说明 |
| --- | --- |
| 名称 | 随便起，方便在列表里认（默认 `shell`） |
| Shell | `bash`（默认）或 `sh` |
| 工作目录 | 留空用设置里的默认值（默认 `/`） |
| SSH 主机 | 选一个已存的主机簿条目，就变成 SSH 会话（不选就是本地 shell） |

打开应用时会**自动回到上次用的会话**（记在浏览器 `localStorage`）。

### 3. 日常操作

- **切换会话**：点左侧列表；或 `Alt+1…9`；或 `Alt+Shift+↑/↓` 上下翻
- **重命名**：双击（或列表项上的编辑入口）改名字，便于区分 `vim`／`htop`／日志
- **关闭会话**：列表项删除按钮，或 `Alt+Shift+W`（会问一句；删的是会话，不是文件）
- **复制**：选中文字自动复制，或 `Ctrl+Shift+C`
- **粘贴**：`Ctrl+Shift+V` 弹确认框（多行内容看清楚再送，防误执行）；
  框里还有「粘贴并回车」；`Shift+Insert` 直接粘贴你系统剪贴板里的内容
- **清屏**：`Alt+Shift+K`
- **断线了**：右上角出现状态点/「重连」按钮，点它即可；通常会自动重连，历史不丢
- **手机上**：底部按键条给 `Esc`、`Tab`、`Ctrl+C`、`Ctrl+D`、`↑`、`End` 和常用符号

### 4. 存一台 SSH 主机

侧栏 **主机簿** → 填：

| 字段 | 例子 | 说明 |
| --- | --- | --- |
| 名称 | `web-01` | 列表里显示的名字 |
| 主机 | `192.168.1.10` 或域名 | 必填 |
| 端口 | `22` | 留空用默认 |
| 用户名 | `root` | 可选 |
| 附加参数 | `-o StrictHostKeyChecking=accept-new` | 可选，原样拼到 `ssh` 命令后 |
| 备注 | `线路 A` | 可选，只给自己看 |

然后新建会话时选这台主机即可。**密码/私钥不落库**：第一次连会问你 `yes`（指纹确认）和密码，
想免密就把公钥放到目标机的 `~/.ssh/authorized_keys`（zterm 以 root 跑，用的是 `/root/.ssh/`）。

### 5. 设置

侧栏 **设置**：默认 shell、默认工作目录、字号、光标闪烁、主题（深/浅）。按账号保存，换设备也在。

---

## 常见问题

**刷新页面/切走再回来，命令会断吗？**
不会。会话和输出都在服务端，重连后整屏恢复。这是本应用存在的理由。

**升级 zterm 或重启应用，会话会丢吗？**
会。会话是内存态（这也是它能"零残留"的原因）。需要跨重启保活就在里面跑 `tmux`/`screen`。

**数据存在哪？要不要备份？**
`/usr/local/apps/@appdata/zterm/data/users/<uid>/`（主机簿、设置）。升级覆盖不到、卸载也不删；
不值得单独备份，但如果里面有几十台主机记录，抄一份就行。

**为什么普通账号打不开？**
桌面入口写的是 `allUsers: false`（只有管理员可见），后端还有一层 `strict` 校验：
非管理员请求一律 403。这是终端应用该有的门槛——它是 root shell。

**能连数据库/串口/其它 NAS 吗？**
能，只要在 shell 里敲得出命令。SSH 只是把 `ssh` 命令做成了一键入口。

**终端的字太小/太大？**
设置里调字号；移动端建议横屏。

**卸载会删我的数据吗？**
不会。卸载走应用中心界面（或 `sudo /usr/local/bin/appcenter-cli uninstall`），
数据目录保留；想彻底清掉：`sudo rm -rf /usr/local/apps/@appdata/zterm`。

---

## 为什么会有这个项目

原先应用市场/社区里有个第三方终端工具 **FntermX**（社区共建版主 EWEDLCM 出品，仓库在 FnDepot 一系），
后来下架了，市面上没有现成的"飞牛官方网关内嵌 + 会话保活"的终端。
所以这个项目从头写了一个：**只做终端本身**，不占端口、不存密码、不搞多用户，尽量少地碰系统。

---

## 工作原理

```
浏览器 ──https──> 飞牛统一网关 :443 ──unix socket──> zterm(root) ──pty──> /bin/bash
                                                        └─ ssh ──> 远程主机
```

- 后端只监听 `${TRIM_APPDEST}/app.sock`（实测 `/usr/local/apps/@appcenter/zterm/app.sock`，
  `/var/apps/zterm/target/app.sock` 是指向它的软链），由网关按 `ui/config` 里的
  `gatewayPrefix` + `gatewaySocket` 转发，所以不需要任何端口。
- 生命周期脚本启动后**等 socket 文件出现再退出**，然后 `chmod 666`
  （网关以别的用户连 socket，权限不够的表现就是"点开一片白"）。
- 前端 `<base href>` 由服务端按请求路径注入，因此挂在 `/app/zterm/` 子路径下资源不会 404。
- 界面资源**内嵌进二进制**（`go:embed`），成品包不存在"界面目录没跟过去"的问题。

### 接口一览

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/health` | 免鉴权：版本、pid、会话数、管理员策略（安装脚本用它自检） |
| GET | `/api/whoami` | 当前身份：`uid`、`isAdmin`、命中的头名（排查网关用） |
| GET/POST | `/api/sessions` | 列会话 / 建会话（`kind: shell` 或 `ssh`，可带 `profileId`） |
| PATCH/DELETE | `/api/sessions/{id}` | 重命名 / 删除 |
| GET | `/api/sessions/{id}/ws` | WebSocket：二进制帧=PTY 输出，文本帧=控制/退出事件 |
| GET/POST | `/api/profiles` | 主机簿 |
| GET/POST | `/api/settings` | 界面设置 |
| GET | `/api/system` | 主机名、负载、内存、磁盘等侧栏信息 |

WebSocket（客户端→服务端，文本帧）：

```json
{"type":"input","data":"ls\r"}
{"type":"resize","cols":120,"rows":40}
{"type":"ping"}
```

服务端→客户端：**二进制帧**是 PTY 原始输出（含 ANSI）；文本帧只有 `{"type":"exit","code":0}` 和 `{"type":"pong"}`。
连上先收整块滚动历史；服务端每 30s 心跳，客户端 90s 无动静会被断开（会话不受影响，重连即可）。

---

## 安全边界（认真地）

- 应用以 **root** 运行（`install_type=root`），本地 shell 就是 **NAS 的 root shell**。这是"管理员终端"的常规形态，但你得知道。
- 可见性靠两处：桌面入口 `allUsers: false`（普通账号看不到图标），以及后端校验。
- `ZTERM_ADMIN_MODE`（默认 **`strict`**，写在 `cmd/main` 里）：
  - `/api/health` 免鉴权、静态资源不卡（里面没秘密）、**其余 `/api/*` 与 WebSocket 逐个判管理员**。
  - `strict`：必须拿到真值管理员标记，否则 403。已在飞牛 **1.2.0701** 核实网关注入的是
    `X-Trim-Isadmin: true`（判定依据）、`X-Trim-Userid`（数据目录名）、`X-Trim-Username`。
    核对方法：打开 `/app/zterm/api/whoami`。
    代价：绕过网关直连 socket 的请求也会被挡（自己调试要带 `-H X-Trim-Isadmin:true`）。
  - `soft`：有标记就必须是管理员，**没标记则放行**（换网关版本头名变了、应用被锁死时用它救急）。
  - `off`：不校验，**仅本机开发**。
- 主机簿**不存密码、不存私钥**；`app.sock` 是 666（网关换用户连接的常态），真正的门槛是上面的身份校验。
- 同账号最多 8 个会话（`internal/session` 的 `MaxPerUID`），单会话滚动历史 512KB；
  PTY 活着会话就一直保留（**关浏览器不会回收**），进程退出后记录再留 10 分钟供回看。

---

## 排错

```bash
# 端到端自检（不用浏览器）：管理员校验（无头/非管理员→403）→ 建会话 → WS → 输入回显
#                          → resize → 断开重连拿回历史 → 删会话
ssh user@your-nas 'python3 -' < deploy/fnos-app/e2e-ws.py

# 状态与手动探活
sudo /usr/local/bin/appcenter-cli status zterm      # running / stopped
ssh user@your-nas 'curl -s --unix-socket /var/apps/zterm/target/app.sock http://localhost/api/health'
ssh user@your-nas 'curl -s -H X-Trim-Isadmin:true --unix-socket /var/apps/zterm/target/app.sock http://localhost/api/sessions'
```

| 现象 | 先看哪里 |
| --- | --- |
| 桌面点开一片空白 | 静态资源是否 404（`<base>`/前缀）；`app.sock` 权限是否 666；进程是否在跑 |
| 应用中心"启动失败" | `/usr/local/apps/@appdata/zterm/lifecycle.log`：多半是 socket 没建出来或二进制没执行位 |
| 装完版本号没变 | 你用了 `install-fpk`，它对已安装应用是空操作 → 改用 `install.sh` |
| 桌面看不到图标 | 只有管理员可见；用管理员登录并刷新桌面 |
| 界面出来了但接口全 403 | 网关没透传身份头，或被普通账号打开 → 看 `/app/zterm/api/whoami` 的 `adminSrc` 是否为空 |
| 会话一刷新就没了 | 后端在重启（升级/停止会结束全部会话）→ 看 lifecycle 日志的启动时间 |
| 粘贴不进去 / 手机没方向键 | 桌面端用 `Ctrl+Shift+V` 或右键；手机用底部按键条 |

---

## 从源码开发

```bash
# 后端（会直接跑起来，PTY 用本地实现；strict 下直连端口会被 403，所以显式关掉校验）
ZTERM_ADMIN_MODE=off go run ./cmd/zterm -addr 127.0.0.1:7791 -data /tmp/zterm-dev -ui webui

# 前端热更新（vite 代理到上面的后端）
cd webui && npm install && npm run dev
```

改完前端记得 `npm run build`（产物进 `internal/webui/dist`，会被 `go:embed` 打进二进制）；
`pack.sh` 在 `webui/node_modules` 存在时会自动构建，`SKIP_UI=1` 跳过。

### 目录结构

```
cmd/zterm/main.go            入口：-data / -socket / -addr / -prefix / -version
internal/auth/               飞牛身份识别（管理员判定、UID 归一、strict/soft/off）
internal/session/            PTY 会话管理：创建/保活/滚动历史（环形缓冲）/重放/回收
internal/profiles/           主机簿与设置落库（按账号分目录）
internal/server/             HTTP + WebSocket 路由、静态资源、网关前缀剥离
internal/webui/              前端嵌入（dist 由 vite 输出到这里）
webui/                       前端源码：index.html / src/{main,api,theme}.js / style.css
tools/mkicon/                Go 画图标（浅底玻璃风），输出 ICON.PNG / ICON_256.PNG / app/ui/images/*
deploy/fnos-app/zterm/       飞牛应用包本体：manifest、cmd/*（生命周期）、config/*、app/ui/*
deploy/fnos-app/pack.sh      打包（图标 → 前端 → linux/amd64 交叉编译 → .fpk）
deploy/fnos-app/install.sh   一键部署 + 自检
deploy/fnos-app/e2e-ws.py    端到端自检（直连 socket，不用浏览器）
```

数据与日志：`/usr/local/apps/@appdata/zterm/data/users/<uid>/`、`.../lifecycle.log`、`.../data/app.log`。

---

## License

尚未指定（如需开源许可，建议 MIT）。欢迎提 Issue / PR。

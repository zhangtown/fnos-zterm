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
| **命令簿** | 140+ 条常用 Linux/飞牛运维命令，**中文标题 + 可一键跑**，不用背命令。带搜索、分类、危险确认；`{容器}` `{目录}` 这种空会列出本机候选值给你选。 |
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

### 方式 A：下载现成的 fpk（最快）

打开 [Releases](https://github.com/zhangtown/fnos-zterm/releases) 下载最新 `zterm.fpk`，然后：

```bash
scp zterm.fpk user@your-nas:/tmp/
ssh user@your-nas
mkdir -p ~/zterm-pkg && tar xzf /tmp/zterm.fpk -C ~/zterm-pkg
sudo /usr/local/bin/appcenter-cli install-local -d ~/zterm-pkg -v 1   # 1 = 装到哪个存储卷
```

升级同理（重新下载 → 再跑一遍 `install-local`）。**不要用 `install-fpk`**，原因见下面的坑。

### 方式 B：从源码打包安装（开发者）

前置：**Go ≥ 1.27**（`go.mod` 声明，本项目只依赖 `creack/pty` 与 `gorilla/websocket`；
CI 发布的包就是按 go.mod 里的版本构建的，老版本 Go 会自动下载对应工具链）、
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

> ⚠️ **两个必须知道的坑**（都是实测踩出来的）
> 1. **`appcenter-cli install-fpk` 对已安装的同名应用是空操作**——哪怕包里版本号更高，
>    它也只打印 `Application [zterm] is installed.`，一个字节都不换。升级必须用 `install-local`。
> 2. **包里不能有 `wizard/` 目录**——非交互安装遇到向导变量会*先卸载再失败*，等于把装好的应用搞掉。

---

**为什么推荐从 Releases 装？** 别人不用装 Go/Node/fnpack——仓库里打 tag 就会自动构建出 fpk（见 [.github/workflows/release.yml](.github/workflows/release.yml)）。

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

侧栏 **设置**：默认 shell、默认工作目录、字号、光标闪烁、主题（深/浅）、命令簿点击行为。按账号保存，换设备也在。

### 6. 用命令簿（不熟 Linux 就看这段）

左侧栏第二个页签「**命令簿**」（或右上角「命令簿」按钮），里面是分好类的常用运维命令，每条都是**中文标题 + 完整命令**。

**基本用法：**

1. 想干什么就搜什么：输入「日志」「磁盘」「docker」「大文件」，支持中文与英文
2. 点一条 → 命令被**填进终端，但不会自动执行**（默认行为）
3. 你自己看一眼，没问题按 `回车`。看懂一条算一条，慢慢就熟了。

想更快也行：设置里把「点命令时」改成「直接执行」；或者对单条命令点它的 ▶ 按钮（每条都带一个）。
**危险命令无论哪种设置都会先弹确认框**（`rm -rf`、`mkfs`、`dd`、`systemctl stop`、`shutdown`……）。

**带空要填的命令**（命令里有 `{}`）：点击后会弹小框让你填，能填的都能**下拉选**，不用自己记：

| 空 | 候选值从哪来 | 例 |
| --- | --- | --- |
| `{容器}` | 本机正在跑的容器 | `docker logs -f {容器}` |
| `{服务}` | systemd 服务名 | `systemctl status {服务}` |
| `{应用}` | 应用中心里的应用 | 看某个应用的日志 |
| `{目录}` | 存储卷 / 共享文件夹 / 一层子目录 | `du -sh {目录}/*` |
| `{用户}` `{端口}` `{镜像}` `{卷}` | 本机现有的 | |

小框里会实时显示 **「将要执行」的那行完整命令**，并挡住 `;` `&` `|` `` ` `` `$` 这类能"改意思"的字符。

**常用分组**：用过的命令会自动排到最上面（最多 6 条），天天用的不用再搜。
**危险操作分组**：默认收起，展开一次就一直展开。
**自己的命令**：「＋ 加一条」写自己的；内置命令不能改但能「**存成我的**」之后再改，或者「**藏起来**」（点「恢复内置」随时找回）。

命令都跑在**当前选中的会话**里，所以先在左边选一个会话（或新建一个），再点命令。
命令簿是按账号存的，每个人各一份。

**里面有什么（举个例，共 12 类）**

| 分类 | 条目标题举例 |
| --- | --- |
| 系统信息与负载 | `飞牛系统版本 + 内核版本`、`吃 CPU 前 10 名`、`温度（CPU / NVMe）`、`实时负载每秒一行` |
| 磁盘与空间 | `各存储卷用量`、`这个卷里哪个一级目录最占空间`、`揪出 1G 以上的大文件`、`所有硬盘健康概览`、`RAID 阵列状态` |
| 文件与目录 | `列目录（含隐藏文件）`、`按文件名/内容找`、`这个目录多大`、`复制/移动/建软链接` |
| 进程与服务 | `按关键词找进程`、`服务状态（上次为啥退出）`、`正在运行的服务清单`、`有没有僵尸进程` |
| 网络与端口 | `本机 IP / 网卡状态`、`监听中的端口（含进程名）`、`这个端口是谁在用`、`连通性测试` |
| Docker | `容器一览`、`进入容器`、`跟着看日志`、`compose 上下线`、`镜像列表` |
| 飞牛 OS 专属 | 飞牛服务状态、应用中心、存储卷/共享文件夹、内置服务日志 |
| 日志排查 | `最新系统日志`、`只看错误`、`上次启动为什么重启`、`按关键词搜日志` |
| 压缩与备份 | `打包成 tar.gz`、`解包`、`rsync 同步`、`校验包完整性` |
| 用户与权限 | `用户/组列表`、`改属主`、`改权限（分值写法）`、`最近登录记录` |
| 危险操作（默认收起） | 重启、关机、停服务、RM 删除、磁盘操作 —— 全都标红 + 执行前确认 |
| 我加的 | 你自己的命令（可填空、可编辑、可删） |

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

**命令簿里的命令跑不动／招不对？**
内置命令是按飞牛的 Debian 基础系统实测过的（其中一部分做了"命令不存在就回退"的处理），
但容器/服务名/存储路径这些东西每台机器不一样 —— 所以带空的地方都能下拉选本机实际值。
命令执行后回显报错时，直接搜关键词找下一条就行了；也可以「存成我的」把它改成合你机器的版本。

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
| GET/POST | `/api/settings` | 界面设置（含命令簿点击行为 `cmdRunMode`） |
| GET/POST | `/api/commands` | 命令簿：列表 / 新增或修改自己的命令 |
| DELETE | `/api/commands/{id}` | 删自己的条目；内置条目则是"藏起来" |
| POST | `/api/commands/{id}/hide` | `{"hidden":true}` 隐藏内置 / `false` 恢复 |
| POST | `/api/commands/{id}/use` | 记一次使用（只影响排序） |
| POST | `/api/commands/reset` | 恢复内置（清隐藏 + 清使用记录，不动自己加的） |
| GET | `/api/commands/candidates?kind=` | 占位符候选值（container/service/app/volume/dir/user/port/image） |
| POST | `/api/commands/run` | 把某条命令直接写进某个会话（危险命令要 `confirm:true`） |
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

# 命令簿自检：15 项（列表/分类/候选值/参数防注入/危险确认/真执行并回读到文件/隐藏与恢复/权限）
scp deploy/fnos-app/cmdbook-selftest.sh user@your-nas:/tmp/ && \
  ssh user@your-nas 'bash /tmp/cmdbook-selftest.sh'

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
| 命令簿点不动 / 提示先建会话 | 命令要发到会话里：先在左侧选一个会话或新建一个 |
| 命令簿里某条命令报错 | 正常：不同机器上服务名/路径会不一样 → 用带下拉候选的命令，或「存成我的」改成本机版本 |

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
internal/commands/           命令簿：内置 140+ 条命令库 + 参数校验 + 本机候选值 + 按账号状态
internal/server/             HTTP + WebSocket 路由、静态资源、网关前缀剥离
internal/webui/              前端嵌入（dist 由 vite 输出到这里）
webui/                       前端源码：index.html / src/{main,commands,state,api,theme}.js / style.css
tools/mkicon/                Go 画图标（浅底玻璃风），输出 ICON.PNG / ICON_256.PNG / app/ui/images/*
deploy/fnos-app/zterm/       飞牛应用包本体：manifest、cmd/*（生命周期）、config/*、app/ui/*
deploy/fnos-app/pack.sh      打包（图标 → 前端 → linux/amd64 交叉编译 → .fpk）
deploy/fnos-app/install.sh   一键部署 + 自检
deploy/fnos-app/e2e-ws.py    端到端自检（直连 socket，不用浏览器）
deploy/fnos-app/cmdbook-selftest.sh  命令簿自检（15 项，含"真跑一条命令并回读结果"）
.github/workflows/release.yml  打 tag 自动构建 fpk 并发 Release
```

### 发版（维护者）

```bash
# tag 即发版：Actions 自动跑 前端构建 → 交叉编译 → fnpack 打包 → 上传 fpk 到 Release
git tag v0.1.3 && git push origin v0.1.3
```

版本号**以 tag 为准**（工作流会把 `deploy/fnos-app/zterm/manifest` 改成 tag 的值再打包），
所以记得把 manifest 里的 `version=` 也提到同一个号，否则本地构建出来的还是旧版本号。
也可以在 Actions 页面手动触发 `release` 工作流并填版本号，用于补发或重跑（同名 Release 会覆盖上传）。

> 顺带一个坑：**fnpack 会重写 manifest**——成品包里的 `manifest` 变成了 `key = value`（空格对齐）、
> CRLF 行尾，还多一行 fnpack 自己算的 `checksum`。所以别把成品包里的 manifest 当源文件，
> 也别想用手写 tar 代替 fnpack（校验值对不上的包装不进去）。

数据与日志：`/usr/local/apps/@appdata/zterm/data/users/<uid>/`、`.../lifecycle.log`、`.../data/app.log`。

---

## License

[MIT](LICENSE) © 2026 zhangtown。欢迎提 Issue / PR。

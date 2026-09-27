package commands

// 内置命令簿。
//
// 每条 = 中文说明（Title）+ 直接可跑的命令（Cmd）+ 可选注意（Note）。
// 命令都在这台飞牛 NAS（Debian 12 + 飞牛 1.2.0701，root 身份）上实测过：
//   - 需要 root 的条目不用写 sudo —— zterm 的会话本来就是 root；
//   - 不在默认 PATH 里的工具（如 smartctl 在 /usr/sbin）写全路径，避免"命令找不到"；
//   - 命令里的 {占位符} 由界面弹框填写，候选值（容器名/服务名/目录…）从本机实时查。
//
// 加新条目：照着下面的写法追加一条即可，Params 会自动从 {占位符} 推出来。

// it 只给说明和命令。
func it(id, cat, title, cmd string) Item {
	return Item{ID: id, Cat: cat, Title: title, Cmd: cmd, Params: deriveParams(cmd)}
}

// itn 多一句注意事项。
func itn(id, cat, title, cmd, note string) Item {
	x := it(id, cat, title, cmd)
	x.Note = note
	return x
}

// itd 危险命令：界面上标红、默认折叠、填之前必须确认。
func itd(id, cat, title, cmd, note string) Item {
	x := itn(id, cat, title, cmd, note)
	x.Danger = true
	return x
}

// def 给某个占位符设预填值。
func def(x Item, name, v string) Item {
	for i := range x.Params {
		if x.Params[i].Name == name {
			x.Params[i].Default = v
		}
	}
	return x
}

var builtinItems = []Item{
	/* ---------------- 系统信息与负载 ---------------- */
	itn("sys-version", CatSys, "飞牛系统版本 + 内核版本",
		`cat /usr/trim/etc/version; uname -sr`,
		"飞牛版本号在 /usr/trim/etc/version（不是 /etc/version）。"),
	it("sys-host", CatSys, "主机名 / 开机时长 / 本次启动时刻",
		`hostname; uptime -p; uptime -s`),
	it("sys-mem", CatSys, "内存用量（含缓存与 swap）",
		`free -h`),
	it("sys-cpu-top", CatSys, "负载 + 吃 CPU 前 10 名",
		`uptime; echo; ps -eo pcpu,pmem,rss,cmd --sort=-pcpu | head -11`),
	it("sys-ram-top", CatSys, "吃内存前 10 名",
		`ps -eo pmem,rss,cmd --sort=-rss | head -11`),
	itn("sys-thermal", CatSys, "温度（CPU 核心 / NVMe 硬盘）",
		`sensors`,
		"lm-sensors 已装。NVMe 那几行是固态温度，>70°C 就该注意散热。"),
	itn("sys-thermal-zone", CatSys, "温度兜底读法（内核 thermal 区）",
		`for z in /sys/class/thermal/thermal_zone*; do printf '%-16s %s°C\n' "$(cat $z/type)" "$(awk "BEGIN{printf \"%.1f\", $(cat $z/temp)/1000}")"; done`,
		"sensors 读不到时用这个。"),
	it("sys-watch-load", CatSys, "实时负载 / 内存 / IO（每秒一行，Ctrl+C 停）",
		`vmstat 1`),
	it("sys-watch-io", CatSys, "磁盘 IO 明细（每秒一行，Ctrl+C 停）",
		`iostat -xm 1`),
	it("sys-htop", CatSys, "交互式监控（彩色，q 退出）",
		`htop`),
	it("sys-boot-time", CatSys, "开机耗时排行（谁拖慢了启动）",
		`systemd-analyze blame | head -15`),
	it("sys-cron", CatSys, "定时任务一览",
		`echo '== root 的 crontab =='; crontab -l 2>/dev/null; echo; echo '== /etc/cron.d =='; ls -l /etc/cron.d 2>/dev/null`),
	it("sys-count", CatSys, "进程 / 线程 / 登录数概览",
		`echo "进程: $(ps -e --no-headers | wc -l)"; echo "线程: $(ps -eLf --no-headers | wc -l)"; echo "登录: $(who | wc -l)"`),

	/* ---------------- 磁盘与空间 ---------------- */
	it("disk-df", CatDisk, "各存储卷用量（按文件系统类型）",
		`df -hT -x tmpfs -x devtmpfs -x overlay`),
	itn("disk-inode", CatDisk, "inode 用量（小文件太多也会写不进去）",
		`df -i -x tmpfs -x devtmpfs`,
		"Use% 满了就是 inode 用尽：文件数太多，清小文件而不是清大文件。"),
	def(itn("disk-vol-top", CatDisk, "这个卷里哪个一级目录最占空间",
		`du -xhd1 {卷} 2>/dev/null | sort -h | tail -15`,
		"-x 表示不跨文件系统（不会把挂载在里面的云盘一起算）。"), "卷", "/vol1"),
	def(it("disk-vol-top2", CatDisk, "再深一层：二级目录排行",
		`du -xhd2 {卷} 2>/dev/null | sort -h | tail -20`), "卷", "/vol1"),
	def(itn("disk-big-file", CatDisk, "揪出 1G 以上的大文件（按大小排）",
		`find {卷} -xdev -type f -size +1G -printf '%s\t%p\n' 2>/dev/null | sort -n | tail -20 | numfmt --to=iec --field=1`,
		"整卷扫描，2TB 大概要跑一两分钟；第一列是大小。"), "卷", "/vol1"),
	def(it("disk-top-file", CatDisk, "这个目录里最大的 20 个文件",
		`find {目录} -xdev -type f -printf '%s\t%p\n' 2>/dev/null | sort -n | tail -20 | numfmt --to=iec --field=1`),
		"目录", "/vol1"),
	def(it("disk-dir-size", CatDisk, "这个目录多大、有多少文件",
		`du -sh {目录} 2>/dev/null; echo "文件数: $(find {目录} -type f 2>/dev/null | wc -l)"`), "目录", "/vol1"),
	it("disk-findmnt", CatDisk, "挂载点一览（含云盘挂载）",
		`findmnt -o TARGET,SOURCE,FSTYPE,SIZE,USED,USE% | head -30`),
	it("disk-lsblk", CatDisk, "块设备与文件系统",
		`lsblk -o NAME,SIZE,FSTYPE,MOUNTPOINT,MODEL`),
	def(itn("disk-smart", CatDisk, "单块硬盘健康（SMART 简况）",
		`/usr/sbin/smartctl -H {设备} | tail -5`,
		"设备名如 /dev/sda、/dev/nvme0n1。要更细就看属性：把 -H 换成 -A。"), "设备", "/dev/sda"),
	itn("disk-smart-all", CatDisk, "所有硬盘的健康概览",
		`for d in /dev/sd? /dev/nvme?n?; do [ -b "$d" ] || continue; printf '%-14s ' "$d"; /usr/sbin/smartctl -H "$d" 2>/dev/null | grep -E 'overall-health|SMART Health Status|SMART overall-health' || echo '（无 SMART 信息，可能是 USB 盘）'; done`,
		"smartctl 在 /usr/sbin 下，所以这里写全路径。"),
	it("disk-raid", CatDisk, "RAID 阵列状态",
		`cat /proc/mdstat; echo; /usr/sbin/mdadm --detail --scan 2>/dev/null`),
	it("disk-var", CatDisk, "/tmp /var 等系统目录占用",
		`du -sh /tmp /var/log /var/apps /root 2>/dev/null | sort -h`),

	/* ---------------- 文件与目录 ---------------- */
	it("file-ls", CatFile, "列目录（含隐藏文件、大小易读）",
		`ls -lah`),
	it("file-ls-new", CatFile, "这个目录里最近改动的文件",
		`ls -lht | head -20`),
	def(it("file-cd", CatFile, "进到某个目录并看内容",
		`cd {目录} && pwd && ls -lah`), "目录", "/vol1"),
	def(itn("file-less", CatFile, "翻看文件内容（q 退出，/ 搜索）",
		`less {文件}`,
		"大日志用它最合适：q 退出、/ 关键字 搜索、G 到末尾、g 回开头。"), "文件", "/etc/hosts"),
	def(it("file-head-tail", CatFile, "看文件开头和结尾",
		`head -20 {文件}; echo '······'; tail -20 {文件}`), "文件", "/etc/hosts"),
	def(it("file-find-name", CatFile, "按文件名找（* 通配）",
		`find {目录} -iname '*{关键词}*' 2>/dev/null | head -50`), "目录", "/vol1"),
	def(it("file-find-content", CatFile, "按内容找（在文本/日志/配置里搜）",
		`grep -rin --include='*.txt' --include='*.md' --include='*.log' --include='*.conf' '{关键词}' {目录} 2>/dev/null | head -50`),
		"目录", "/vol1"),
	def(it("file-count", CatFile, "这个目录有多少文件",
		`find {目录} -type f 2>/dev/null | wc -l`), "目录", "/vol1"),
	it("file-tree", CatFile, "看两层目录结构",
		`find . -maxdepth 2 -printf '%y %p\n' 2>/dev/null | head -60`),
	def(it("file-stat", CatFile, "看权限 / 属主 / 时间 / 大小",
		`stat -c '权限=%A 属主=%U:%G 大小=%s 字节 修改=%y 名称=%n' {路径}; echo; ls -ld {路径}`), "路径", "/vol1"),
	def(it("file-sha256", CatFile, "算文件校验和（比对两份是否一致）",
		`sha256sum {文件}`), "文件", "/etc/hosts"),
	def(it("file-wc", CatFile, "数行数 / 单词数",
		`wc -l {文件}`), "文件", "/etc/hosts"),
	def(it("file-cp", CatFile, "复制文件或目录（保留属性）",
		`cp -av {源} {目标}`), "源", "/vol1/1000"),
	def(it("file-mv", CatFile, "移动 / 改名",
		`mv -v {源} {目标}`), "源", ""),
	def(it("file-ln", CatFile, "建软链接（快捷方式）",
		`ln -sv {源} {链接}`), "源", ""),

	/* ---------------- 进程与服务 ---------------- */
	it("proc-ps", CatProc, "按关键词找进程",
		`ps aux | grep -i '{关键词}' | grep -v grep`),
	it("proc-top", CatProc, "top 一次性快照（进程明细）",
		`top -b -n1 | head -22`),
	it("proc-detail", CatProc, "看某进程的明细（内存/线程/运行时长）",
		`ps -o pid,ppid,user,pcpu,pmem,rss,nlwp,etime,cmd -p {PID}`),
	it("proc-tree", CatProc, "进程树（父子关系）",
		`pstree -p | head -40`),
	it("proc-fd", CatProc, "某进程打开了哪些文件",
		`lsof -p {PID} | head -30`),
	it("proc-zombie", CatProc, "有没有僵尸进程",
		`ps -eo stat,pid,cmd | awk '$1 ~ /Z/ {print}'`),
	def(itn("proc-svc-status", CatProc, "服务状态（是否在跑、上次为啥退出）",
		`systemctl status {服务} --no-pager -l | head -25`,
		"下拉里前 20 个是飞牛自家服务（trim_* / dlcenter / mediasrv / share_service …），后面是本机在跑的其它服务。"), "服务", "trim_main"),
	it("proc-svc-failed", CatProc, "启动失败的服务",
		`systemctl --failed --no-pager`),
	it("proc-svc-running", CatProc, "正在运行的服务清单",
		`systemctl list-units --type=service --state=running --plain --no-legend`),
	it("proc-svc-enabled", CatProc, "开机自启清单",
		`systemctl list-unit-files --state=enabled --no-pager | head -60`),
	it("proc-timers", CatProc, "系统自带的定时任务（timer）",
		`systemctl list-timers --no-pager | head -20`),

	/* ---------------- 网络与端口 ---------------- */
	it("net-ip", CatNet, "本机 IP / 网卡状态",
		`ip -br a`),
	it("net-route", CatNet, "路由与默认网关",
		`ip route`),
	it("net-dns", CatNet, "当前 DNS 服务器",
		`grep -v '^#' /etc/resolv.conf`),
	it("net-listen", CatNet, "监听中的端口（含进程名）",
		`ss -ltnp`),
	it("net-estab", CatNet, "对外的已建立连接",
		`ss -tnp state established | head -40`),
	itn("net-who-port", CatNet, "这个端口是谁在用",
		`ss -ltnp | grep -E ':({端口})($|\s)'; echo; lsof -nP -iTCP:{端口} -sTCP:LISTEN 2>/dev/null | head`,
		"端口候选来自本机当前监听列表。"),
	it("net-stat", CatNet, "网卡收发包 / 错误统计",
		`ip -s link`),
	def(it("net-ping", CatNet, "连通性测试（4 个包）",
		`ping -c 4 {主机}`), "主机", "223.5.5.5"),
	def(it("net-dns-lookup", CatNet, "域名解析测试",
		`getent hosts {域名}; echo; dig +short {域名}`), "域名", "baidu.com"),
	def(it("net-http", CatNet, "测网址能不能通（状态码 + 耗时）",
		`curl -sS -o /dev/null -w 'HTTP %{http_code} · 用时 %{time_total}s · 收到 %{size_download} 字节\n' {网址}`),
		"网址", "https://www.baidu.com"),
	def(it("net-http-head", CatNet, "看响应头（重定向、缓存、服务器）",
		`curl -sSI {网址} | head -20`), "网址", "https://www.baidu.com"),
	it("net-smb-who", CatNet, "谁在连着 SMB 共享",
		`smbstatus | head -40`),
	it("net-firewall", CatNet, "防火墙规则（nftables）",
		`nft list ruleset 2>/dev/null | head -40`),

	/* ---------------- Docker 容器 ---------------- */
	it("docker-ps", CatDocker, "运行中的容器",
		`docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'`),
	it("docker-psa", CatDocker, "全部容器（含已停止）",
		`docker ps -a --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'`),
	it("docker-images", CatDocker, "镜像列表与体积",
		`docker images --format 'table {{.Repository}}\t{{.Tag}}\t{{.Size}}'`),
	def(it("docker-logs-f", CatDocker, "跟容器日志（Ctrl+C 退出）",
		`docker logs -f --tail=100 {容器}`), "容器", ""),
	def(it("docker-logs", CatDocker, "看容器日志尾部（200 行）",
		`docker logs --tail=200 {容器} 2>&1 | tail -200`), "容器", ""),
	def(it("docker-exec", CatDocker, "进容器里看看（sh；exit 退出）",
		`docker exec -it {容器} sh`), "容器", ""),
	def(itn("docker-inspect", CatDocker, "容器关键配置（重启策略 / 挂载 / 网络）",
		`docker inspect -f '镜像={{.Config.Image}} 重启策略={{.HostConfig.RestartPolicy.Name}} 网络={{.HostConfig.NetworkMode}}\n挂载={{range .Mounts}}{{.Source}}→{{.Destination}} {{end}}' {容器}`,
		"排查「容器莫名其妙没了」先看这一条：重启策略若是 no，NAS 重启后它就不会自己起来。"), "容器", ""),
	def(it("docker-ip", CatDocker, "容器 IP",
		`docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' {容器}`), "容器", ""),
	def(it("docker-compose-src", CatDocker, "这个容器的 compose 文件在哪",
		`docker inspect -f '项目={{index .Config.Labels "com.docker.compose.project"}} 文件={{index .Config.Labels "com.docker.compose.project.config_files"}}' {容器}`),
		"容器", ""),
	def(it("docker-cp-out", CatDocker, "从容器里拷文件出来",
		`docker cp {容器}:{容器内路径} {本地目录}`), "容器", ""),
	it("docker-stats", CatDocker, "容器资源占用（一次快照）",
		`docker stats --no-stream`),
	it("docker-df", CatDocker, "Docker 占了多少空间",
		`docker system df`),
	it("docker-networks", CatDocker, "Docker 网络列表",
		`docker network ls`),
	it("docker-volumes", CatDocker, "Docker 卷列表",
		`docker volume ls | head -30`),
	def(it("docker-pull", CatDocker, "拉取镜像",
		`docker pull {镜像}`), "镜像", ""),
	def(itn("docker-compose-up", CatDocker, "按 compose 文件启动 / 更新容器",
		`cd {目录} && docker compose up -d`,
		"会按 compose 文件重建有改动的容器；先 docker compose config 看一眼确认没改错。"), "目录", "/vol1/docker"),

	/* ---------------- 飞牛 OS 专属 ---------------- */
	itn("fnos-version", CatFnOS, "飞牛系统版本",
		`cat /usr/trim/etc/version; uname -sr`,
		"飞牛版本号文件是 /usr/trim/etc/version。"),
	it("fnos-apps", CatFnOS, "装了哪些应用（目录）",
		`ls /var/apps`),
	itn("fnos-appcenter-list", CatFnOS, "应用中心：已装应用与版本",
		`/usr/local/bin/appcenter-cli list`,
		"这个 CLI 权限是 0700 root-only，普通 SSH 账号跑不了；zterm 里是 root，直接能用。"),
	def(it("fnos-appcenter-status", CatFnOS, "应用中心：某个应用的运行状态",
		`/usr/local/bin/appcenter-cli status {应用}`), "应用", "zterm"),
	def(itn("fnos-app-log-f", CatFnOS, "跟某个应用的日志（Ctrl+C 退出）",
		`tail -n 100 -f /var/log/apps/{应用}.log`,
		"日志文件名就是应用名，例如 1Panel.log、baidu.netdisk.log。"), "应用", "1Panel"),
	it("fnos-app-logs", CatFnOS, "哪些应用日志最近在写",
		`ls -lht /var/log/apps/*.log 2>/dev/null | head -20`),
	itn("fnos-svc", CatFnOS, "飞牛核心服务是否正常（一行一个）",
		`systemctl is-active trim_main trim_nginx trim_open_gateway trim_app_center trim_diskpowerd dlcenter mediasrv imagesrv filestor_service share_service dockermgr smbd nmbd`,
		"active = 正常。哪个不是 active，就用「服务状态」那条去看原因。"),
	it("fnos-gateway", CatFnOS, "统一网关与 nginx（应用入口那一层）",
		`systemctl status trim_open_gateway trim_nginx --no-pager | head -30`),
	itn("fnos-gateway-log", CatFnOS, "网关日志（应用打不开时看这里）",
		`tail -n 50 /var/log/trim_open_gateway/info.log`,
		"同目录下还有 error.log。飞牛桌面里点应用没反应，通常这里能看到原因。"),
	it("fnos-volumes", CatFnOS, "存储卷列表与用量",
		`ls -d /vol* 2>/dev/null; echo; df -hT | grep -E 'vol|Filesystem'`),
	def(itn("fnos-shares", CatFnOS, "共享文件夹一览（按用户 id 分目录）",
		`ls -l {卷}/1000`,
		"飞牛的共享文件夹放在各存储卷的 1000 目录下（1000 = 第一个用户的 uid）。"), "卷", "/vol1"),
	def(it("fnos-share-usage", CatFnOS, "共享文件夹各占多大",
		`du -xhd1 {卷}/1000 2>/dev/null | sort -h`), "卷", "/vol1"),
	it("fnos-smb", CatFnOS, "谁在连着 SMB 共享",
		`smbstatus | head -40`),
	itn("fnos-download", CatFnOS, "下载中心状态与最近任务",
		`systemctl is-active dlcenter; echo; ls -lht '/vol1/1000/下载' 2>/dev/null | head -10`,
		"下载目录名按你的共享设置，不对就改路径。"),
	it("fnos-users", CatFnOS, "有哪些飞牛用户",
		`getent passwd | awk -F: '$3>=1000 && $3<65534 {printf "%-12s uid=%-6s home=%s\n",$1,$3,$6}'`),
	itd("fnos-install-pkg", CatFnOS, "手动装 / 升级一个离线应用包（进阶）",
		`/usr/local/bin/appcenter-cli install-local -d {目录} -v 1`,
		"会先停旧版本再安装；目录里必须是解包后的应用（含 manifest）。日常用应用中心就够了。"),

	/* ---------------- 日志排查 ---------------- */
	it("log-follow", CatLog, "系统日志实时滚动（Ctrl+C 退出）",
		`journalctl -f -n 100`),
	it("log-err", CatLog, "本次开机以来的错误级日志",
		`journalctl -p err -b --no-pager | tail -60`),
	it("log-recent", CatLog, "最近 1 小时的日志",
		`journalctl --since '1 hour ago' --no-pager | tail -100`),
	def(it("log-svc", CatLog, "某个服务的日志（最近 200 行）",
		`journalctl -u {服务} -n 200 --no-pager`), "服务", "trim_main"),
	def(it("log-svc-f", CatLog, "跟某个服务的日志",
		`journalctl -u {服务} -f -n 50`), "服务", "trim_main"),
	itn("log-kernel", CatLog, "内核 / 硬件消息（掉盘、USB、网卡）",
		`dmesg -T | tail -60`,
		"-T 是给人看的时间格式；硬盘掉线、重新识别在这里看。"),
	it("log-oom", CatLog, "有没有进程因内存不足被杀",
		`journalctl -k --since '7 days ago' --no-pager | grep -iE 'oom|killed process|out of memory' | tail -20`),
	it("log-size", CatLog, "日志占了多少空间",
		`journalctl --disk-usage; echo; du -sh /var/log/* 2>/dev/null | sort -h | tail -15`),
	it("log-auth", CatLog, "登录记录（谁登录过、有没有人试密码）",
		`last -n 20; echo '--- ssh 日志 ---'; journalctl -u ssh -n 30 --no-pager | tail -30`),
	it("log-disk-err", CatLog, "磁盘 / 文件系统报错",
		`dmesg -T | grep -iE 'ata[0-9]|i/o error|ext4-fs error|btrfs|nvme.*error|reset' | tail -30`),

	/* ---------------- 压缩与备份 ---------------- */
	def(it("backup-tar-create", CatBackup, "打包目录成 .tar.gz",
		`tar -czf {输出}.tar.gz {目录}`), "目录", "/vol1/1000"),
	def(it("backup-tar-list", CatBackup, "看包里有什么（不解包）",
		`tar -tzf {文件} | head -50`), "文件", ""),
	def(it("backup-tar-extract", CatBackup, "解包到指定目录",
		`mkdir -p {目录} && tar -xzf {文件} -C {目录}`), "目录", "/vol1"),
	def(it("backup-zip-create", CatBackup, "打成 zip",
		`zip -r {输出}.zip {目录}`), "目录", "/vol1/1000"),
	def(itn("backup-zip-extract", CatBackup, "解 zip（中文名不乱码）",
		`unzip -O cp936 -d {目录} {文件}`,
		"Windows 打的 zip 用 -O cp936，否则中文文件名会变乱码。"), "目录", "/vol1"),
	def(itn("backup-rsync-dry", CatBackup, "rsync 预演：只报告要改什么（不动数据）",
		`rsync -avhn --delete {源}/ {目标}/`,
		"同步之前先跑这个：确认要删/要传的清单没错，再去掉 n 真跑。"), "源", "/vol1/1000"),
	itd("backup-rsync", CatBackup, "rsync 实际同步（目标端多出来的会被删）",
		`rsync -avh --delete --info=progress2 {源}/ {目标}/`,
		"注意结尾的斜杠：{源}/ 是同步目录里的内容，{源} 是连目录本身一起同步。删东西是不可逆的，先跑预演那条。"),

	/* ---------------- 用户与权限 ---------------- */
	it("user-list", CatUser, "有哪些账号",
		`getent passwd | awk -F: '$3>=1000 || $1=="root" {printf "%-12s uid=%-6s home=%-18s shell=%s\n",$1,$3,$6,$7}'`),
	it("user-who", CatUser, "谁在线 / 最近登录",
		`who; echo '--- 最近登录 ---'; last -n 20`),
	def(it("user-id", CatUser, "某个账号属于哪些组",
		`id {用户}`), "用户", "root"),
	it("user-me", CatUser, "我现在是谁（zterm 里是 root）",
		`id; echo; echo "本终端以 $(id -un) 身份运行，通常不需要 sudo"`),
	def(it("user-perm", CatUser, "看权限 / 属主 / 大小",
		`stat -c '权限=%A 属主=%U:%G 大小=%s 字节 修改=%y 名称=%n' {路径}`), "路径", "/vol1"),
	def(itn("user-owner-list", CatUser, "看目录里各文件的属主（数字 uid）",
		`ls -lnh {目录} | head -30`,
		"显示的是数字 uid；用「有哪些账号」那条把 uid 翻成名字。"), "目录", "/vol1/1000"),
	it("user-ssh-key", CatUser, "本机 root 的免密公钥 / 私钥",
		`ls -l /root/.ssh 2>/dev/null; echo '--- 对方机器上授权过的公钥 ---'; awk '{print $1, $3}' /root/.ssh/authorized_keys 2>/dev/null; echo '--- 本机私钥 ---'; ls /root/.ssh/id_* 2>/dev/null`),
	it("user-ssh-keygen", CatUser, "生成一对新密钥（免密登录用，一路回车）",
		`ssh-keygen -t ed25519 -N '' -f /root/.ssh/id_ed25519`),
	def(it("user-ssh-copy", CatUser, "把本机公钥装到对方机器（配免密）",
		`ssh-copy-id {用户}@{主机}`), "用户", "root"),

	/* ---------------- 危险操作 ---------------- */
	itd("danger-reboot", CatDanger, "重启 NAS",
		`reboot`,
		"所有会话、服务一并中断，大约 1～2 分钟后才回来（本页会断线）。"),
	itd("danger-poweroff", CatDanger, "关机",
		`poweroff`,
		"关机后需要人按电源键或 Wake-on-LAN 才能开机。"),
	itd("danger-shutdown-later", CatDanger, "1 分钟后重启（可用下一条取消）",
		`shutdown -r +1`,
		"先把要停的服务停干净；后悔了就在 1 分钟内执行「取消计划重启」。"),
	it("danger-shutdown-cancel", CatDanger, "取消已计划的重启 / 关机",
		`shutdown -c`),
	def(itd("danger-svc-stop", CatDanger, "停止服务",
		`systemctl stop {服务}`,
		"停掉的是真实服务：共享/相册/网关停了，对应功能立刻不可用。"), "服务", "smbd"),
	def(itd("danger-svc-restart", CatDanger, "重启服务",
		`systemctl restart {服务}`,
		"会话不断，但该服务会短暂不可用。"), "服务", "dlcenter"),
	def(itd("danger-svc-disable", CatDanger, "禁止服务开机自启",
		`systemctl disable {服务}`,
		"改的是开机行为，当前这次运行不受影响。"), "服务", "smbd"),
	itd("danger-kill", CatDanger, "强制杀进程（kill -9）",
		`kill -9 {PID}`,
		"-9 不给进程保存数据的机会；先试 kill {PID}（不带 -9）更稳妥。"),
	itd("danger-pkill", CatDanger, "按名字批量杀进程",
		`pkill -9 -f '{关键词}'`,
		"匹配范围可能超出预期：先用「按关键词找进程」确认名单。"),
	def(itd("danger-rm", CatDanger, "删除文件 / 目录（不可恢复）",
		`rm -rf {路径}`,
		"rm 没有回收站。路径里有变量或通配符时先 echo 一遍看展开结果。"), "路径", "/vol1/1000/待删除"),
	def(itd("danger-docker-rm", CatDanger, "删除容器",
		`docker rm -f {容器}`,
		"容器里的临时数据一起没了；数据卷（volumes）不在删除范围。"), "容器", ""),
	def(itd("danger-docker-rmi", CatDanger, "删除镜像",
		`docker rmi {镜像}`,
		"依赖这个镜像的容器必须先删，否则删不掉。"), "镜像", ""),
	itd("danger-docker-prune", CatDanger, "清理 Docker 垃圾（未使用的镜像/网络/缓存）",
		`docker system prune -a`,
		"会删掉所有没被容器使用的镜像（下次用要重新拉/重新构建）。加 --volumes 连数据卷一起删，更狠。"),
	itd("danger-journal-vacuum", CatDanger, "清理系统日志（只留 7 天）",
		`journalctl --vacuum-time=7d`,
		"排障前别清：清掉就查不到历史原因了。"),
	def(itd("danger-chown", CatDanger, "改属主（递归）",
		`chown -R {用户}:{用户} {路径}`,
		"改错属主会让服务读不了自己的文件；共享目录一般属于 1000。"), "用户", "root"),
	def(itd("danger-chmod", CatDanger, "改权限（递归）",
		`chmod -R 755 {路径}`,
		"755 = 属主可读写执行、其他人可读可执行。0777 别用。"), "路径", "/vol1/1000"),
	itd("danger-mkfs", CatDanger, "格式化磁盘 / 分区（先看清设备再动手）",
		`lsblk -o NAME,SIZE,FSTYPE,MOUNTPOINT,MODEL  # 看清设备名后再手动执行：mkfs.ext4 /dev/XXX`,
		"这一步会把整块盘清零，且无法撤销。真要格式化就走飞牛的存储管理界面，别在终端里赌。"),
	itd("danger-dd", CatDanger, "覆写磁盘 / 分区（数据清零，不可恢复）",
		`lsblk -o NAME,SIZE,FSTYPE  # 示例：dd if=/dev/zero of=/dev/sdX bs=1M status=progress  ← 别在不确定时执行`,
		"of= 写错一个字母就是另一块盘。涉及数据恢复请先断电、再找专业工具。"),
	itd("danger-drop-cache", CatDanger, "释放内存缓存（不影响数据，短时会变慢）",
		`sync; echo 3 > /proc/sys/vm/drop_caches`,
		"free 里那几 GB 是缓存，本来就不算「被占用」；只想看真实占用的话没必要执行。"),
}

// builtinByID 给"隐藏/恢复内置命令"做校验。
var builtinByID = map[string]Item{}

func init() {
	seen := map[string]bool{}
	for i := range builtinItems {
		x := builtinItems[i]
		if x.Params == nil {
			x.Params = deriveParams(x.Cmd)
			builtinItems[i] = x
		}
		if x.ID == "" || seen[x.ID] {
			panic("命令簿里有重复或空的 id: " + x.ID)
		}
		seen[x.ID] = true
		builtinByID[x.ID] = x
	}
}

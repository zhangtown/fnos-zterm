package commands

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Candidates 返回某个占位符的候选值。
//
// 全部在本机实时查（容器名、服务名、存储卷、共享目录、当前监听端口…），
// 任何查询失败都返回空列表 —— 候选只是帮忙，填命令的手动输入永远可用。
func Candidates(kind string) []string {
	switch kind {
	case "container":
		out := runLines(4*time.Second, "docker", "ps", "-a", "--format", "{{.Names}}")
		return clean(out, 300)
	case "image":
		out := runLines(4*time.Second, "docker", "images", "--format", "{{.Repository}}:{{.Tag}}")
		var keep []string
		for _, s := range out {
			if !strings.HasPrefix(s, "<none>") && s != ":" {
				keep = append(keep, s)
			}
		}
		return clean(keep, 300)
	case "service":
		return serviceNames()
	case "app":
		return listNames("/var/apps")
	case "volume":
		return Volumes()
	case "user":
		return userNames()
	case "dir":
		return dirCandidates()
	case "port":
		return listenPorts()
	}
	return nil
}

// Volumes 返回存储卷（如 /vol1、/vol2，含根）。飞牛的存储卷与共享文件夹查询都依赖它。
func Volumes() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	if ents, err := os.ReadDir("/"); err == nil {
		for _, e := range ents {
			if e.IsDir() && strings.HasPrefix(e.Name(), "vol") {
				add("/" + e.Name())
			}
		}
	}
	// 多卷可能挂在 /vol1/vol2 这种层级上，findmnt 兜底
	for _, l := range runLines(2*time.Second, "findmnt", "-rno", "TARGET") {
		if strings.HasPrefix(l, "/vol") {
			add(l)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return []string{"/"}
	}
	if out[0] != "/" {
		out = append([]string{"/"}, out...)
	}
	return out
}

// dirCandidates 常用目录：共享文件夹、应用目录、日志目录…
func dirCandidates() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p == "" || seen[p] || len(out) >= 60 {
			return
		}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, vol := range Volumes() {
		add(vol)
		userDir := filepath.Join(vol, "1000")
		add(userDir)
		if ents, err := os.ReadDir(userDir); err == nil {
			for _, e := range ents {
				if e.IsDir() {
					add(filepath.Join(userDir, e.Name()))
				}
			}
		}
	}
	for _, p := range []string{"/var/log/apps", "/var/log", "/var/apps", "/etc", "/root", "/tmp", "/usr/trim"} {
		add(p)
	}
	return out
}

// serviceNames 服务名候选：正在跑的 + 启动失败的 + 飞牛自家的（后两类即使没跑也可能要排查）。
func serviceNames() []string {
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		n = strings.TrimSuffix(strings.TrimSpace(n), ".service")
		if n == "" || seen[n] || len(out) >= 200 {
			return
		}
		seen[n] = true
		out = append(out, n)
	}
	for _, state := range []string{"running", "failed"} {
		lines := runLines(3*time.Second, "systemctl", "list-units", "--type=service",
			"--state="+state, "--plain", "--no-legend", "--no-pager")
		for _, l := range lines {
			if f := strings.Fields(l); len(f) > 0 {
				add(f[0])
			}
		}
	}
	for _, n := range fnosServices {
		add(n)
	}
	return out
}

// fnosServices 飞牛自家服务：即使当前没跑也常要查状态（统一网关、应用中心、共享、下载、相册…）。
var fnosServices = []string{
	"trim_main", "trim_nginx", "trim_open_gateway", "trim_app_center", "trim_diskpowerd",
	"dlcenter", "mediasrv", "imagesrv", "filestor_service", "share_service",
	"dockermgr", "smbd", "nmbd", "ssh",
}

func userNames() []string {
	var out []string
	for _, l := range runLines(3*time.Second, "getent", "passwd") {
		f := strings.Split(l, ":")
		if len(f) < 3 {
			continue
		}
		uid, err := strconv.Atoi(f[2])
		if err != nil {
			continue
		}
		if f[0] == "root" || (uid >= 1000 && uid < 65534) {
			out = append(out, f[0])
		}
	}
	return clean(out, 100)
}

// listenPorts 当前本机在监听的端口（数字，升序）。
func listenPorts() []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range runLines(2*time.Second, "ss", "-ltnH") {
		f := strings.Fields(l)
		if len(f) < 4 {
			continue
		}
		addr := f[3] // Local Address:Port
		i := strings.LastIndex(addr, ":")
		if i < 0 {
			continue
		}
		p := addr[i+1:]
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := strconv.Atoi(out[i])
		b, _ := strconv.Atoi(out[j])
		return a < b
	})
	return out
}

func listNames(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return clean(out, 300)
}

func runLines(timeout time.Duration, name string, args ...string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	var res []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			res = append(res, l)
		}
	}
	return res
}

// clean 去重 + 截断（候选项再长也不该把前端撑爆）。
func clean(in []string, max int) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
		if len(out) >= max {
			break
		}
	}
	return out
}

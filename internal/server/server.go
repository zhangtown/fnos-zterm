// Package server 提供 HTTP + WebSocket 服务。
//
// 在飞牛 NAS 上以内嵌应用方式运行时不监听任何 TCP 端口：飞牛统一网关把
// /app/<应用名>/... 的请求转发到 $TRIM_APPDEST/app.sock（unix socket）。
// 同时也支持 --addr 直连模式，方便本地开发或反代。
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"zterm/internal/auth"
	"zterm/internal/profiles"
	"zterm/internal/session"
)

// Config 是服务启动参数。
//
// 界面资源二选一：UIFS（二进制内嵌，正式发布用）或 StaticDir（磁盘目录，开发调试用）。
type Config struct {
	SocketPath string // 非空则监听 unix socket（内嵌应用模式）
	Addr       string // 否则监听 TCP 地址
	StaticDir  string // 前端静态资源目录
	UIFS       fs.FS  // 嵌入的前端资源（优先于 StaticDir）
	UIName     string // 界面来源描述，只用于日志
	DataDir    string
	Version    string
}

// Server 持有路由、会话管理器与配置存储。
type Server struct {
	cfg    Config
	mgr    *session.Manager
	store  *profiles.Store
	ui     *staticFiles
	http   *http.Server
	start  time.Time
	upgrade websocket.Upgrader
}

// New 组装服务（不开始监听）。
func New(cfg Config) (*Server, error) {
	var (
		ui  *staticFiles
		err error
	)
	if cfg.UIFS != nil {
		name := cfg.UIName
		if name == "" {
			name = "内嵌界面"
		}
		ui, err = newStaticFS(cfg.UIFS, name)
	} else {
		ui, err = newStaticFiles(cfg.StaticDir)
	}
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:   cfg,
		mgr:   session.NewManager(512),
		store: profiles.NewStore(cfg.DataDir + "/users"),
		ui:    ui,
		start: time.Now(),
		upgrade: websocket.Upgrader{
			ReadBufferSize:  16 << 10,
			WriteBufferSize: 16 << 10,
			// 网关是同源转发；直连调试时也允许任意来源，因为真正的门槛是
			// 管理员身份（离线局域网 + 内嵌模式 + allUsers=false）。
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	mux := http.NewServeMux()
	// 健康检查必须免鉴权：安装脚本会在没有身份头的环境下 curl 它。
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/whoami", s.guard(s.handleWhoami))

	mux.HandleFunc("GET /api/config", s.guard(s.handleConfig))
	mux.HandleFunc("GET /api/system", s.guard(s.handleSystem))

	mux.HandleFunc("GET /api/sessions", s.guard(s.handleSessionsList))
	mux.HandleFunc("POST /api/sessions", s.guard(s.handleSessionsCreate))
	mux.HandleFunc("PATCH /api/sessions/{id}", s.guard(s.handleSessionsPatch))
	mux.HandleFunc("DELETE /api/sessions/{id}", s.guard(s.handleSessionsDelete))
	mux.HandleFunc("GET /api/sessions/{id}/ws", s.handleAttach)

	mux.HandleFunc("GET /api/profiles", s.guard(s.handleProfilesList))
	mux.HandleFunc("POST /api/profiles", s.guard(s.handleProfilesUpsert))
	mux.HandleFunc("DELETE /api/profiles/{id}", s.guard(s.handleProfilesDelete))

	mux.HandleFunc("GET /api/settings", s.guard(s.handleSettingsGet))
	mux.HandleFunc("POST /api/settings", s.guard(s.handleSettingsSave))

	mux.Handle("/", ui)

	s.http = &http.Server{
		Handler:           stripAppPrefix(mux),
		ReadHeaderTimeout: 15 * time.Second,
		// 终端会话可能是长连接（挂着不动），不能设 WriteTimeout。
		IdleTimeout: 120 * time.Second,
		ErrorLog:    log.New(os.Stderr, "http: ", log.LstdFlags),
	}

	// 定期清理已结束的会话；启动时也顺带清理上个进程留下的僵死记录（内存态，无残留）。
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for range t.C {
			s.mgr.Reap()
		}
	}()
	return s, nil
}

// guard 包装需要"管理员 + 身份"的接口。
func (s *Server) guard(fn func(http.ResponseWriter, *http.Request, auth.Identity)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := auth.CheckAdmin(r); err != nil {
			writeErr(w, http.StatusForbidden, err.Error())
			return
		}
		fn(w, r, auth.Identify(r))
	}
}

// UISource 返回界面资源来源（启动日志/排查用）。
func (s *Server) UISource() string {
	return fmt.Sprintf("%s（%d 个文件）", s.ui.Source(), s.ui.len())
}

// MaxSessions 返回每账号的会话上限。
func (s *Server) MaxSessions() int { return s.mgr.MaxPerUID }

// Listen 按配置开始监听，返回实际监听地址。
func (s *Server) Listen() (string, error) {
	if s.cfg.SocketPath != "" {
		// 卸载旧 socket，避免 "address already in use"
		_ = os.Remove(s.cfg.SocketPath)
		ln, err := net.Listen("unix", s.cfg.SocketPath)
		if err != nil {
			return "", err
		}
		// 0666：网关进程以其它用户身份连进来是常态（别锁成 0600）。
		if err := os.Chmod(s.cfg.SocketPath, 0o666); err != nil {
			log.Printf("警告: chmod socket 失败: %v", err)
		}
		go func() { _ = s.http.Serve(ln) }()
		return "unix:" + s.cfg.SocketPath, nil
	}

	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return "", err
	}
	go func() { _ = s.http.Serve(ln) }()
	return "http://" + ln.Addr().String(), nil
}

// Shutdown 优雅退出。
func (s *Server) Shutdown(ctx context.Context) {
	s.mgr.CloseAll()
	_ = s.http.Shutdown(ctx)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	defer io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20)) //nolint:errcheck
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("请求体解析失败: %w", err)
	}
	return nil
}

// ---------- 基础接口 ----------

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	total, alive := s.mgr.Count()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"app":     "zterm",
		"version": s.cfg.Version,
		"uptime":  int(time.Since(s.start).Seconds()),
		"pid":     os.Getpid(),
		"go":      runtime.Version(),
		"sessions": map[string]int{
			"total": total,
			"alive": alive,
		},
		"adminMode": auth.AdminMode(),
		"time":      time.Now().Format(time.RFC3339),
	})
}

// handleWhoami 把网关透传的请求头原样回显：核实 uid/管理员头名字就用它。
func (s *Server) handleWhoami(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	headers := map[string]string{}
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	writeJSON(w, http.StatusOK, map[string]any{
		"uid":        id.SafeUID(),
		"isAdmin":    id.IsAdmin,
		"adminClaim": id.AdminClaim,
		"adminSrc":   id.AdminSrc,
		"uidHint":    id.UIDSrc,
		"adminMode":  auth.AdminMode(),
		"headers":    headers,
		"headerKeys": keys,
		"dataDir":    s.store.UserDir(id.SafeUID()),
	})
}

// handleConfig 给前端下发运行期配置。
func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request, id auth.Identity) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version":        s.cfg.Version,
		"uid":            id.SafeUID(),
		"isAdmin":        id.IsAdmin,
		"adminMode":      auth.AdminMode(),
		"scrollbackKB":   512,
		"maxSessions":    s.mgr.MaxPerUID,
		"embedded":       s.cfg.SocketPath != "",
		"addr":           s.cfg.Addr,
		"serverTime":     time.Now().Format(time.RFC3339),
		"defaultShell":   profiles.DefaultSettings().DefaultShell,
		"dataDirForUser": s.store.UserDir(id.SafeUID()),
	})
}

func (s *Server) handleSystem(w http.ResponseWriter, _ *http.Request, _ auth.Identity) {
	host, _ := os.Hostname()
	writeJSON(w, http.StatusOK, map[string]any{
		"hostname": host,
		"go":       runtime.Version(),
		"os":       runtime.GOOS + "/" + runtime.GOARCH,
		"cpus":     runtime.NumCPU(),
		"shells":   availableShells(),
		"pid":      os.Getpid(),
	})
}

func availableShells() []string {
	out := []string{}
	for _, sh := range []string{"/bin/bash", "/bin/sh", "/bin/zsh", "/bin/ash", "/usr/bin/fish"} {
		if fi, err := os.Stat(sh); err == nil && !fi.IsDir() {
			out = append(out, sh)
		}
	}
	if len(out) == 0 {
		out = append(out, "/bin/sh")
	}
	return out
}

// ---------- 会话接口 ----------

func (s *Server) handleSessionsList(w http.ResponseWriter, _ *http.Request, id auth.Identity) {
	list := s.mgr.List(id.SafeUID())
	if list == nil {
		list = []session.Info{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"list": list, "max": s.mgr.MaxPerUID})
}

type createReq struct {
	Kind       string   `json:"kind"` // shell | ssh
	Name       string   `json:"name"`
	Shell      string   `json:"shell"`
	Cwd        string   `json:"cwd"`
	Cols       uint16   `json:"cols"`
	Rows       uint16   `json:"rows"`
	SSH        sshReq   `json:"ssh"`
	ProfileID  string   `json:"profileId"` // 直接从主机簿创建
}

type sshReq struct {
	Host  string   `json:"host"`
	Port  int      `json:"port"`
	User  string   `json:"user"`
	Extra []string `json:"extra"`
}

func (s *Server) handleSessionsCreate(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req createReq
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	uid := id.SafeUID()
	settings := s.store.Settings(uid)

	opt := session.Options{
		Kind:  session.KindShell,
		Name:  req.Name,
		UID:   uid,
		Shell: req.Shell,
		Cwd:   req.Cwd,
		Cols:  req.Cols,
		Rows:  req.Rows,
	}
	if opt.Shell == "" {
		opt.Shell = settings.DefaultShell
	}
	if opt.Cwd == "" {
		opt.Cwd = settings.DefaultCwd
	}

	switch req.Kind {
	case "", "shell":
	case "ssh":
		opt.Kind = session.KindSSH
		opt.SSH = session.SSHTarget{Host: req.SSH.Host, Port: req.SSH.Port, User: req.SSH.User, Extra: req.SSH.Extra}
		// 用主机簿里的条目建会话时，以主机簿为准（前端只传 id）。
		if req.ProfileID != "" {
			if p, ok := s.findProfile(uid, req.ProfileID); ok {
				opt.SSH = session.SSHTarget{Host: p.Host, Port: p.Port, User: p.User, Extra: p.Extra}
			}
		}
		if _, err := profiles.Normalize(profiles.Profile{
			Name: opt.Name, Host: opt.SSH.Host, Port: opt.SSH.Port, User: opt.SSH.User, Extra: opt.SSH.Extra,
		}, 0); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "kind 只能是 shell 或 ssh")
		return
	}

	sess, err := s.mgr.Create(opt)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sess.Info())
}

func (s *Server) findProfile(uid, id string) (profiles.Profile, bool) {
	list, err := s.store.Profiles(uid)
	if err != nil {
		return profiles.Profile{}, false
	}
	for _, p := range list {
		if p.ID == id {
			return p, true
		}
	}
	return profiles.Profile{}, false
}

func (s *Server) handleSessionsPatch(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.mgr.Rename(id.SafeUID(), r.PathValue("id"), req.Name) {
		writeErr(w, http.StatusNotFound, "会话不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSessionsDelete(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	if !s.mgr.Kill(id.SafeUID(), r.PathValue("id")) {
		writeErr(w, http.StatusNotFound, "会话不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- WebSocket ----------

func (s *Server) handleAttach(w http.ResponseWriter, r *http.Request) {
	// WS 握手不走 guard（浏览器 WebSocket 带不了自定义头，身份全靠网关透传的 cookie/头），
	// 所以这里自己判一次管理员。
	if err := auth.CheckAdmin(r); err != nil {
		writeErr(w, http.StatusForbidden, err.Error())
		return
	}
	uid := auth.Identify(r).SafeUID()
	sess, ok := s.mgr.Get(uid, r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "会话不存在")
		return
	}

	conn, err := s.upgrade.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade 失败: %v", err)
		return
	}
	defer conn.Close()

	cols, rows := parseSize(r.URL.Query().Get("cols"), r.URL.Query().Get("rows"))
	sess.Resize(cols, rows)
	snapshot, ch, detach := sess.Attach()
	defer detach()

	info := sess.Info()
	_ = conn.WriteJSON(map[string]any{
		"type":       "ready",
		"id":         info.ID,
		"name":       info.Name,
		"kind":       info.Kind,
		"target":     info.Target,
		"alive":      info.Alive,
		"exitCode":   info.ExitCode,
		"cols":       info.Cols,
		"rows":       info.Rows,
		"scrollback": info.Scrollback,
		"clients":    info.Clients,
	})
	if len(snapshot) > 0 {
		if err := conn.WriteMessage(websocket.BinaryMessage, snapshot); err != nil {
			return
		}
	}

	// 写协程：把会话输出推给浏览器；channel 关闭（会话结束/客户端掉队）即收尾。
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return
				}
				if msg.Data != nil {
					if err := conn.WriteMessage(websocket.BinaryMessage, msg.Data); err != nil {
						return
					}
					continue
				}
				if msg.Event == "exit" {
					_ = conn.WriteJSON(map[string]any{"type": "exit", "code": msg.Code})
					return
				}
			case <-ticker.C:
				// 服务端心跳：经过网关的转发链路可能静默清掉空闲连接。
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
					return
				}
			}
		}
	}()

	conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	go func() {
		defer conn.Close()
		for {
			typ, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			switch typ {
			case websocket.TextMessage:
				var ctl struct {
					Type string `json:"type"`
					Cols uint16 `json:"cols"`
					Rows uint16 `json:"rows"`
					Data string `json:"data"`
				}
				if json.Unmarshal(data, &ctl) != nil {
					continue
				}
				switch ctl.Type {
				case "resize", "size":
					sess.Resize(ctl.Cols, ctl.Rows)
				case "input":
					if ctl.Data != "" {
						_ = sess.Write([]byte(ctl.Data))
					}
				case "ping":
					_ = conn.WriteJSON(map[string]any{"type": "pong", "t": time.Now().UnixMilli()})
				}
			default:
				if err := sess.Write(data); err != nil {
					return
				}
			}
		}
	}()

	<-done
}

func parseSize(cols, rows string) (uint16, uint16) {
	toU16 := func(s string, def uint16) uint16 {
		n := 0
		for _, c := range s {
			if c < '0' || c > '9' || n > 100000 {
				return def
			}
			n = n*10 + int(c-'0')
		}
		if n <= 0 || n > 1000 {
			return def
		}
		return uint16(n)
	}
	return toU16(cols, 100), toU16(rows, 30)
}

// ---------- 主机簿 ----------

func (s *Server) handleProfilesList(w http.ResponseWriter, _ *http.Request, id auth.Identity) {
	list, err := s.store.Profiles(id.SafeUID())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []profiles.Profile{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"list": list})
}

func (s *Server) handleProfilesUpsert(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var p profiles.Profile
	if err := decodeJSON(r, &p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := s.store.Upsert(id.SafeUID(), p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if list == nil {
		list = []profiles.Profile{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"list": list})
}

func (s *Server) handleProfilesDelete(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	list, err := s.store.Delete(id.SafeUID(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []profiles.Profile{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"list": list})
}

// ---------- 偏好设置 ----------

func (s *Server) handleSettingsGet(w http.ResponseWriter, _ *http.Request, id auth.Identity) {
	writeJSON(w, http.StatusOK, s.store.Settings(id.SafeUID()))
}

func (s *Server) handleSettingsSave(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var st profiles.Settings
	if err := decodeJSON(r, &st); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := s.store.SaveSettings(id.SafeUID(), st)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// ctxOriginalPath 保存请求到达时的原始路径（剥离应用前缀之前）。
type ctxKey int

const ctxOriginalPath ctxKey = 1

// stripAppPrefix —— 飞牛统一网关把内嵌应用挂在 /app/<应用名>/ 下，转发过来的是**完整路径**
// （参考实现 FN-Terminal 就是直接注册 /app/fn-terminal/ 的）。
// 这里把 /app/<名字> 剥掉再交给内部路由：应用改名/别名安装都不用改代码，
// 同时把原始路径留在 context 里，静态资源要用它注入 <base href>。
func stripAppPrefix(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orig := r.URL.Path
		if stripped, ok := stripPrefix(orig); ok {
			r.URL.Path = stripped
		}
		r = r.WithContext(context.WithValue(r.Context(), ctxOriginalPath, orig))
		next.ServeHTTP(w, r)
	})
}

// stripPrefix 返回剥离后的路径与是否发生剥离。
func stripPrefix(p string) (string, bool) {
	if !strings.HasPrefix(p, "/app/") {
		return p, false
	}
	rest := strings.TrimPrefix(p, "/app/")
	i := strings.Index(rest, "/")
	if i < 0 {
		return "/", true // 就是 /app/<名字>
	}
	return rest[i:], true
}

// originalPath 取原始请求路径；没有经过剥离时就是当前路径。
func originalPath(r *http.Request) string {
	if p, ok := r.Context().Value(ctxOriginalPath).(string); ok && p != "" {
		return p
	}
	return r.URL.Path
}

package session

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Kind 区分会话背后跑的是什么。
type Kind string

const (
	KindShell Kind = "shell" // 本地登录 shell
	KindSSH   Kind = "ssh"   // 由 NAS 上的 ssh 客户端建立的外连会话
)

// SSHTarget 描述一个 ssh 目标。密码/口令不在应用里保存，全部在终端里交互输入。
type SSHTarget struct {
	Host  string   `json:"host"`
	Port  int      `json:"port,omitempty"`
	User  string   `json:"user,omitempty"`
	Extra []string `json:"extra,omitempty"`
}

func (t SSHTarget) display() string {
	u := t.User
	if u == "" {
		u = "root"
	}
	host := t.Host
	if t.Port > 0 && t.Port != 22 {
		host = fmt.Sprintf("%s:%d", host, t.Port)
	}
	return u + "@" + host
}

func (t SSHTarget) args() []string {
	args := []string{"-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=3"}
	if t.Port > 0 && t.Port != 22 {
		args = append(args, "-p", strconv.Itoa(t.Port))
	}
	args = append(args, t.Extra...)
	user := t.User
	if user == "" {
		user = "root"
	}
	return append(args, "--", user+"@"+t.Host)
}

// Options 创建一个会话所需的全部参数。
type Options struct {
	Kind       Kind
	Name       string
	UID        string
	Shell      string
	Cwd        string
	SSH        SSHTarget
	Cols       uint16
	Rows       uint16
	Scrollback int
	Env        []string // 额外环境变量，形如 KEY=VALUE
}

// Info 是会话的可序列化快照（HTTP 列表接口用）。
type Info struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Kind       Kind       `json:"kind"`
	Target     string     `json:"target,omitempty"`
	Cwd        string     `json:"cwd,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	Alive      bool       `json:"alive"`
	ExitCode   int        `json:"exitCode"`
	EndedAt    *time.Time `json:"endedAt,omitempty"`
	Clients    int        `json:"clients"`
	Cols       uint16     `json:"cols"`
	Rows       uint16     `json:"rows"`
	Scrollback int        `json:"scrollback"`
}

// Message 是推给订阅者的一个消息：Data 非空表示终端输出（二进制帧），
// 否则是控制事件（exit）。
type Message struct {
	Data  []byte
	Event string
	Code  int
}

// Session 是一个活着的（或刚结束的）终端会话。
type Session struct {
	id        string
	name      string
	uid       string
	kind      Kind
	target    string
	cwd       string
	createdAt time.Time

	mu         sync.Mutex
	ptmx       *os.File
	cmd        *exec.Cmd
	ring       *Ring
	subs       map[chan Message]struct{}
	cols, rows uint16
	alive      bool
	killed     bool
	exitCode   int
	endedAt    time.Time
}

// New 启动一个会话：分配 pty、拉起子进程、开始读输出。
func New(opt Options) (*Session, error) {
	if opt.Cols == 0 {
		opt.Cols = 100
	}
	if opt.Rows == 0 {
		opt.Rows = 30
	}
	if opt.UID == "" {
		opt.UID = "local"
	}

	var cmd *exec.Cmd
	var target string
	switch opt.Kind {
	case KindSSH:
		if strings.TrimSpace(opt.SSH.Host) == "" {
			return nil, errors.New("缺少 ssh 主机地址")
		}
		cmd = exec.Command("ssh", opt.SSH.args()...)
		target = opt.SSH.display()
	case KindShell, "":
		opt.Kind = KindShell
		shell := opt.Shell
		if shell == "" {
			shell = "/bin/bash"
		}
		if _, err := os.Stat(shell); err != nil {
			shell = "/bin/sh"
		}
		// -l 让它成为登录 shell：PATH、/etc/profile 里的环境与 prompt 才与 ssh 登录一致。
		cmd = exec.Command(shell, "-l")
	default:
		return nil, fmt.Errorf("不支持的会话类型 %q", opt.Kind)
	}

	cwd := opt.Cwd
	if fi, err := os.Stat(cwd); err != nil || !fi.IsDir() {
		cwd = "/"
	}
	cmd.Dir = cwd

	env := os.Environ()
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
	if os.Getenv("LANG") == "" {
		env = append(env, "LANG=C.UTF-8")
	}
	env = append(env, opt.Env...)

	s := &Session{
		id:        newID(),
		name:      opt.Name,
		uid:       opt.UID,
		kind:      opt.Kind,
		target:    target,
		cwd:       cwd,
		createdAt: time.Now(),
		ring:      NewRing(opt.Scrollback),
		subs:      map[chan Message]struct{}{},
		cols:      opt.Cols,
		rows:      opt.Rows,
		alive:     true,
	}
	env = append(env, "ZTERM_SESSION="+s.id)
	cmd.Env = env

	ptmx, err := startPTY(cmd, opt.Cols, opt.Rows)
	if err != nil {
		return nil, err
	}
	s.ptmx = ptmx
	s.cmd = cmd
	if s.name == "" {
		if s.kind == KindSSH {
			s.name = "ssh " + target
		} else {
			s.name = "shell"
		}
	}

	go s.readLoop()
	go s.waitLoop()
	return s, nil
}

// ID 返回会话 id。
func (s *Session) ID() string { return s.id }

// UID 返回会话归属的飞牛账号 uid。
func (s *Session) UID() string { return s.uid }

// Kind 返回会话类型。
func (s *Session) Kind() Kind { return s.kind }

func (s *Session) readLoop() {
	buf := make([]byte, 32<<10)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			s.mu.Lock()
			s.ring.Write(chunk)
			for ch := range s.subs {
				select {
				case ch <- Message{Data: chunk}:
				default:
					// 客户端读得太慢：踢掉它，让它重连后从环形缓冲里补历史，
					// 比把内存堆起来（或拖慢整个会话）都更划算。
					close(ch)
					delete(s.subs, ch)
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) waitLoop() {
	err := s.cmd.Wait()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	now := time.Now()

	s.mu.Lock()
	s.alive = false
	s.exitCode = code
	s.endedAt = now
	if s.ptmx != nil {
		s.ptmx.Close()
		s.ptmx = nil
	}
	for ch := range s.subs {
		select {
		case ch <- Message{Event: "exit", Code: code}:
		default:
		}
		close(ch)
		delete(s.subs, ch)
	}
	s.mu.Unlock()
}

// Write 把键盘输入写进 pty。
func (s *Session) Write(p []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.alive || s.ptmx == nil {
		return errors.New("会话已结束")
	}
	_, err := s.ptmx.Write(p)
	return err
}

// Resize 更新窗口大小（前端每次 fit 都会调）。
func (s *Session) Resize(cols, rows uint16) {
	if cols == 0 || rows == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cols, s.rows = cols, rows
	if s.alive && s.ptmx != nil {
		_ = resizePTY(s.ptmx, cols, rows)
	}
}

// Attach 订阅一个客户端：先返回滚动历史，再返回后续消息的通道。
// 取消订阅必须调用 detach（由 cancel 闭包完成）。
func (s *Session) Attach() (snapshot []byte, ch <-chan Message, cancel func()) {
	sub := make(chan Message, 256)
	s.mu.Lock()
	snapshot = s.ring.Snapshot()
	alive := s.alive
	if alive {
		s.subs[sub] = struct{}{}
	}
	code := s.exitCode
	s.mu.Unlock()

	if !alive {
		// 已经结束的会话：立刻给出 exit 事件，前端会显示退出码。
		close(sub)
		done := make(chan struct{})
		close(done)
		out := make(chan Message, 1)
		out <- Message{Event: "exit", Code: code}
		close(out)
		return snapshot, out, func() {}
	}
	var once sync.Once
	return snapshot, sub, func() {
		once.Do(func() {
			s.mu.Lock()
			if _, ok := s.subs[sub]; ok {
				close(sub)
				delete(s.subs, sub)
			}
			s.mu.Unlock()
		})
	}
}

// Kill 结束会话：先关主端（子进程收到 SIGHUP，正常退出），3 秒后仍在则杀进程组。
func (s *Session) Kill() {
	s.mu.Lock()
	if !s.alive {
		s.mu.Unlock()
		return
	}
	s.killed = true
	f := s.ptmx
	pid := 0
	if s.cmd != nil && s.cmd.Process != nil {
		pid = s.cmd.Process.Pid
	}
	s.mu.Unlock()

	if f != nil {
		_ = f.Close()
	}
	if pid > 0 {
		go func() {
			time.Sleep(3 * time.Second)
			s.mu.Lock()
			alive := s.alive
			s.mu.Unlock()
			if alive {
				killGroup(pid)
			}
		}()
	}
}

// Rename 改会话显示名。
func (s *Session) Rename(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if len(name) > 60 {
		name = name[:60]
	}
	s.mu.Lock()
	s.name = name
	s.mu.Unlock()
}

// Info 返回当前快照。
func (s *Session) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	info := Info{
		ID:         s.id,
		Name:       s.name,
		Kind:       s.kind,
		Target:     s.target,
		Cwd:        s.cwd,
		CreatedAt:  s.createdAt,
		Alive:      s.alive,
		ExitCode:   s.exitCode,
		Clients:    len(s.subs),
		Cols:       s.cols,
		Rows:       s.rows,
		Scrollback: s.ring.Len(),
	}
	if !s.endedAt.IsZero() {
		t := s.endedAt
		info.EndedAt = &t
	}
	return info
}

// Manager 管理全部会话。会话按 uid 归属，跨账号不可见也不可操作。
type Manager struct {
	mu         sync.Mutex
	sessions   map[string]*Session
	Scrollback int
	MaxPerUID  int
	Retain     time.Duration
}

// NewManager 创建管理器。scrollbackKB 为每个会话保留的滚动历史大小。
func NewManager(scrollbackKB int) *Manager {
	if scrollbackKB <= 0 {
		scrollbackKB = 512
	}
	return &Manager{
		sessions:   map[string]*Session{},
		Scrollback: scrollbackKB << 10,
		MaxPerUID:  8,
		Retain:     10 * time.Minute,
	}
}

// Create 新建会话，名字按需自动去重。
func (m *Manager) Create(opt Options) (*Session, error) {
	m.mu.Lock()
	live := 0
	for _, s := range m.sessions {
		if s.uid == opt.UID && s.alive {
			live++
		}
	}
	if live >= m.MaxPerUID {
		m.mu.Unlock()
		return nil, fmt.Errorf("同时最多 %d 个会话，请先关闭一些", m.MaxPerUID)
	}
	opt.Name = m.uniqueName(opt.Name)
	opt.Scrollback = m.Scrollback
	m.mu.Unlock()

	s, err := New(opt)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.sessions[s.id] = s
	m.mu.Unlock()
	return s, nil
}

func (m *Manager) uniqueName(base string) string {
	if strings.TrimSpace(base) == "" {
		base = "shell"
	}
	taken := map[string]bool{}
	for _, s := range m.sessions {
		taken[s.name] = true
	}
	if !taken[base] {
		return base
	}
	for i := 2; i < 1000; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		if !taken[cand] {
			return cand
		}
	}
	return base + "-" + strconv.FormatInt(time.Now().Unix()%10000, 10)
}

// Get 取一个会话，uid 不匹配时按"不存在"处理（避免探测到别人的会话）。
func (m *Manager) Get(uid, id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok || s.uid != uid {
		return nil, false
	}
	return s, true
}

// List 返回某个账号的全部会话，新的在前。
func (m *Manager) List(uid string) []Info {
	m.mu.Lock()
	out := make([]Info, 0, len(m.sessions))
	for _, s := range m.sessions {
		if s.uid != uid {
			continue
		}
		out = append(out, s.Info())
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// Count 返回存活会话数（health 用）。
func (m *Manager) Count() (total, alive int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		total++
		if s.alive {
			alive++
		}
	}
	return
}

// Kill 结束一个会话并立即从列表里移除。
func (m *Manager) Kill(uid, id string) bool {
	s, ok := m.Get(uid, id)
	if !ok {
		return false
	}
	s.Kill()
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
	return true
}

// Rename 改会话名。
func (m *Manager) Rename(uid, id, name string) bool {
	s, ok := m.Get(uid, id)
	if !ok {
		return false
	}
	s.Rename(name)
	return true
}

// Reap 清理已经结束一段时间的会话，避免列表无限增长。
func (m *Manager) Reap() {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		s.mu.Lock()
		dead := !s.alive && !s.endedAt.IsZero() && now.Sub(s.endedAt) > m.Retain
		s.mu.Unlock()
		if dead {
			delete(m.sessions, id)
		}
	}
}

// CloseAll 结束所有会话（进程退出前调用）。
func (m *Manager) CloseAll() {
	m.mu.Lock()
	list := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	m.mu.Unlock()
	for _, s := range list {
		s.Kill()
	}
}

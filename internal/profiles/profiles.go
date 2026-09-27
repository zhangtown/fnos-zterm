// Package profiles 保存每个飞牛账号自己的 SSH 主机列表与界面偏好。
//
// 目录布局（$TRIM_PKGVAR 即数据目录，升级/卸载默认不动）：
//
//	<data>/users/<uid>/profiles.json   SSH 主机
//	<data>/users/<uid>/settings.json   界面偏好
//
// 只存"连到哪、用谁连"这类信息；**口令不落盘**，一律在终端里交互输入。
package profiles

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Profile 是一个 SSH 主机条目。
type Profile struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Host      string    `json:"host"`
	Port      int       `json:"port,omitempty"`
	User      string    `json:"user,omitempty"`
	Extra     []string  `json:"extra,omitempty"` // 额外 ssh 参数，如 -o StrictHostKeyChecking=no
	Note      string    `json:"note,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

// Settings 是界面偏好，按账号独立保存。
type Settings struct {
	DefaultShell string `json:"defaultShell,omitempty"`
	DefaultCwd   string `json:"defaultCwd,omitempty"`
	FontSize     int    `json:"fontSize,omitempty"`
	Theme        string `json:"theme,omitempty"` // dark | light
	CursorBlink  *bool  `json:"cursorBlink,omitempty"`
	ScrollbackKB int    `json:"scrollbackKB,omitempty"`
}

// DefaultSettings 返回一份可用的默认值。
func DefaultSettings() Settings {
	blink := true
	return Settings{
		DefaultShell: "/bin/bash",
		DefaultCwd:   "/",
		FontSize:     14,
		Theme:        "dark",
		CursorBlink:  &blink,
	}
}

// Store 是文件存储。root 一般是数据目录下的 users/。
type Store struct{ root string }

// NewStore 创建存储并把根目录建好（0700）。
func NewStore(root string) *Store {
	_ = os.MkdirAll(root, 0o700)
	return &Store{root: root}
}

// Root 返回存储根目录（README/诊断用）。
func (s *Store) Root() string { return s.root }

var uidRe = regexp.MustCompile(`^[0-9A-Za-z_.-]{1,32}$`)

// dir 返回某个账号的目录并确保存在。uid 已在上游白名单校验过，这里再挡一次。
func (s *Store) dir(uid string) (string, error) {
	if !uidRe.MatchString(uid) {
		return "", fmt.Errorf("非法 uid: %q", uid)
	}
	d := filepath.Join(s.root, uid)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}

// readJSON 读文件；不存在时返回零值且不报错。
func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, v)
}

// writeJSON 原子写：临时文件 0600 → rename，避免升级/断电时留下半个文件。
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Profiles 读取某个账号的主机列表。
func (s *Store) Profiles(uid string) ([]Profile, error) {
	d, err := s.dir(uid)
	if err != nil {
		return nil, err
	}
	var list []Profile
	if err := readJSON(filepath.Join(d, "profiles.json"), &list); err != nil {
		return nil, err
	}
	return list, nil
}

// SaveProfiles 覆盖保存主机列表（并顺手规范化每条记录）。
func (s *Store) SaveProfiles(uid string, list []Profile) error {
	d, err := s.dir(uid)
	if err != nil {
		return err
	}
	out := make([]Profile, 0, len(list))
	for i := range list {
		p, err := Normalize(list[i], i)
		if err != nil {
			return err
		}
		out = append(out, p)
	}
	return writeJSON(filepath.Join(d, "profiles.json"), out)
}

// Upsert 新增或更新一条主机记录，返回保存后的列表。
func (s *Store) Upsert(uid string, p Profile) ([]Profile, error) {
	list, err := s.Profiles(uid)
	if err != nil {
		return nil, err
	}
	np, err := Normalize(p, len(list))
	if err != nil {
		return nil, err
	}
	replaced := false
	for i := range list {
		if list[i].ID == np.ID {
			np.UpdatedAt = time.Now()
			list[i] = np
			replaced = true
			break
		}
	}
	if !replaced {
		np.UpdatedAt = time.Now()
		list = append(list, np)
	}
	if err := s.SaveProfiles(uid, list); err != nil {
		return nil, err
	}
	return s.Profiles(uid)
}

// Delete 删除一条主机记录。
func (s *Store) Delete(uid, id string) ([]Profile, error) {
	list, err := s.Profiles(uid)
	if err != nil {
		return nil, err
	}
	out := list[:0]
	for _, p := range list {
		if p.ID != id {
			out = append(out, p)
		}
	}
	if err := s.SaveProfiles(uid, out); err != nil {
		return nil, err
	}
	return s.Profiles(uid)
}

// hostRe 允许域名、IPv4、IPv6 字面量；不允许以 "-" 开头（否则会被 ssh 当成选项）。
var hostRe = regexp.MustCompile(`^[A-Za-z0-9._](\[?[A-Za-z0-9:._-]*\]?)$`)

// Normalize 校验并补齐一条记录。
func Normalize(p Profile, index int) (Profile, error) {
	p.Host = strings.TrimSpace(p.Host)
	p.User = strings.TrimSpace(p.User)
	p.Name = strings.TrimSpace(p.Name)
	p.Note = strings.TrimSpace(p.Note)

	if p.Host == "" {
		return p, errors.New("主机地址不能为空")
	}
	if strings.HasPrefix(p.Host, "-") || !hostRe.MatchString(p.Host) {
		return p, fmt.Errorf("主机地址 %q 不合法", p.Host)
	}
	if strings.HasPrefix(p.User, "-") || strings.ContainsAny(p.User, " \t\n@") {
		return p, fmt.Errorf("用户名 %q 不合法", p.User)
	}
	if p.Port < 0 || p.Port > 65535 {
		return p, fmt.Errorf("端口 %d 不合法", p.Port)
	}
	p.Extra = sanitizeArgs(p.Extra)
	if len(p.Extra) > 8 {
		p.Extra = p.Extra[:8]
	}
	if p.Name == "" {
		if p.User != "" {
			p.Name = p.User + "@" + p.Host
		} else {
			p.Name = p.Host
		}
	}
	if p.ID == "" {
		p.ID = fmt.Sprintf("p%d", index+1)
		for i := 0; i < 8; i++ {
			if _, err := os.Stat("/dev/null"); err != nil { // 只为让 ID 生成不依赖随机源
				break
			}
			break
		}
		p.ID = p.ID + "-" + fmt.Sprint(time.Now().UnixNano()%1000000)
	}
	return p, nil
}

// sanitizeArgs 去掉空项、控制字符与可能破坏配置的字符。
func sanitizeArgs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		a = strings.TrimSpace(a)
		if a == "" || strings.ContainsAny(a, "\n\r\t\x00'\"`$;&|") {
			continue
		}
		out = append(out, a)
	}
	return out
}

// Settings 读取界面偏好，缺失字段用默认值补齐。
func (s *Store) Settings(uid string) Settings {
	st := DefaultSettings()
	d, err := s.dir(uid)
	if err != nil {
		return st
	}
	_ = readJSON(filepath.Join(d, "settings.json"), &st)
	return st
}

// SaveSettings 保存界面偏好（做范围收敛，避免前端传进来离谱的值）。
func (s *Store) SaveSettings(uid string, st Settings) (Settings, error) {
	d, err := s.dir(uid)
	if err != nil {
		return st, err
	}
	cur := s.Settings(uid)
	if st.DefaultShell != "" {
		if strings.HasPrefix(st.DefaultShell, "/") && len(st.DefaultShell) < 120 {
			cur.DefaultShell = st.DefaultShell
		}
	}
	if st.DefaultCwd != "" {
		if strings.HasPrefix(st.DefaultCwd, "/") && len(st.DefaultCwd) < 240 {
			cur.DefaultCwd = st.DefaultCwd
		}
	}
	if st.FontSize >= 8 && st.FontSize <= 32 {
		cur.FontSize = st.FontSize
	}
	if st.Theme == "dark" || st.Theme == "light" {
		cur.Theme = st.Theme
	}
	if st.CursorBlink != nil {
		cur.CursorBlink = st.CursorBlink
	}
	if st.ScrollbackKB >= 64 && st.ScrollbackKB <= 8192 {
		cur.ScrollbackKB = st.ScrollbackKB
	}
	if err := writeJSON(filepath.Join(d, "settings.json"), cur); err != nil {
		return cur, err
	}
	return cur, nil
}

// UserDir 返回某个账号的数据目录（不创建）。
func (s *Store) UserDir(uid string) string {
	if !uidRe.MatchString(uid) {
		uid = "local"
	}
	return filepath.Join(s.root, uid)
}

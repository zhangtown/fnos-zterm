// Package commands 实现"命令簿"：一批可一键填进终端的命令片段，附中文说明。
//
// 设计取舍：
//   - 内置库写在代码里（builtin.go）。升级版本就自带新命令，不需要迁移数据。
//   - 落盘只保存"用户的改动"：{hidden: [内置 id], items: [自定义条目], meta: {用过几次}}。
//     这样升级不会覆盖用户自己加的 / 自己藏的。
//   - 内置条目可以隐藏（不物理删除），用户想恢复就"恢复内置"。
//   - 命令里的 {占位符} 由前端弹框填写；候选值（容器名/服务名/目录…）由后端在本机实时查。
package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 分类 id
const (
	CatSys    = "sys"
	CatDisk   = "disk"
	CatFile   = "file"
	CatProc   = "proc"
	CatNet    = "net"
	CatDocker = "docker"
	CatFnOS   = "fnos"
	CatLog    = "log"
	CatBackup = "backup"
	CatUser   = "user"
	CatDanger = "danger"
	CatMine   = "mine"
)

// Category 一个分类。
type Category struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Categories 展示顺序（前端按此顺序渲染）。
var Categories = []Category{
	{CatSys, "系统信息与负载"},
	{CatDisk, "磁盘与空间"},
	{CatFile, "文件与目录"},
	{CatProc, "进程与服务"},
	{CatNet, "网络与端口"},
	{CatDocker, "Docker 容器"},
	{CatFnOS, "飞牛 OS 专属"},
	{CatLog, "日志排查"},
	{CatBackup, "压缩与备份"},
	{CatUser, "用户与权限"},
	{CatDanger, "危险操作"},
	{CatMine, "我加的"},
}

// Param 命令里的一处 {占位符}。
type Param struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"` // text|dir|container|image|service|app|volume|user|port
	Default string   `json:"default,omitempty"`
	Options []string `json:"options,omitempty"` // 非 text 时由后端在本机查出候选项
}

// Item 一条命令。
type Item struct {
	ID     string  `json:"id"`
	Cat    string  `json:"cat"`
	Title  string  `json:"title"`
	Cmd    string  `json:"cmd"`
	Note   string  `json:"note,omitempty"`
	Danger bool    `json:"danger,omitempty"`
	Params []Param `json:"params,omitempty"`

	// 以下由 Store 合并写入，落盘时不看这些字段。
	Builtin    bool   `json:"builtin"`
	Used       int    `json:"used,omitempty"`
	LastUsedAt string `json:"lastUsedAt,omitempty"`
}

// Meta 使用记录。
type Meta struct {
	Used       int    `json:"used"`
	LastUsedAt string `json:"lastUsedAt"`
}

// State 是落盘的原始状态。
type State struct {
	Hidden []string          `json:"hidden"` // 被隐藏的内置 id
	Custom []Item            `json:"items"`  // 用户自己加的
	Meta   map[string]Meta   `json:"meta"`
}

// Response 是给前端的完整视图。
type Response struct {
	List []Item          `json:"list"`
	Meta map[string]Meta `json:"meta"`
	Cats []Category      `json:"cats"`
}

// ---------- 占位符 ----------

// goTmplRe 是 docker --format / go 模板里的 {{...}}，必须先摘掉，
// 否则 {{.Names}} 里的第二个花括号会被当成占位符的开头（实测踩过这个坑）。
var goTmplRe = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// phRe 占位符：{中文名} / {目录} 这种。
var phRe = regexp.MustCompile(`\{([^{}\n]{1,24})\}`)

// 常见占位符 → 候选项类型。不在表里的按 text（自由填写）处理。
var paramKinds = map[string]string{
	"容器": "container", "容器名": "container",
	"镜像": "image", "镜像名": "image",
	"服务": "service", "服务名": "service",
	"应用": "app", "应用名": "app",
	"卷": "volume", "存储卷": "volume",
	"用户": "user", "账号": "user",
	"端口": "port",
	"目录": "dir", "路径": "dir", "文件夹": "dir", "本地目录": "dir", "输出目录": "dir",
}

// hasPlaceholder 摘掉 go 模板后再看有没有没填的占位符。
func hasPlaceholder(cmd string) bool {
	return phRe.MatchString(goTmplRe.ReplaceAllString(cmd, ""))
}

// deriveParams 从命令里抽出 {占位符}（去重、保序）。
//
// 注意：docker 的 {{.Names}}、{{range .Mounts}} 这类模板先被摘掉 —— 实测发现
// "{{.Names}}" 的第二个 { 配上后面的 } 会被误判成占位符 ".Names"。
func deriveParams(cmd string) []Param {
	cmd = goTmplRe.ReplaceAllString(cmd, "")
	if !strings.Contains(cmd, "{") {
		return nil
	}
	seen := map[string]bool{}
	var out []Param
	for _, m := range phRe.FindAllStringSubmatch(cmd, -1) {
		name := strings.TrimSpace(m[1])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		kind := paramKinds[name]
		if kind == "" {
			kind = "text"
		}
		out = append(out, Param{Name: name, Kind: kind})
	}
	return out
}

// ---------- 危险判定 ----------

// dangerRe 只用于"用户自己新加的条目"：明显会删数据/停服务/重启/格式化的命令自动标红。
// 内置条目在 builtin.go 里显式标注（所以像 systemctl stop 这种内置的也是准的）。
var dangerRe = regexp.MustCompile(`(?i)(^|[^\w])rm\s+-[a-z]*[rf]` +
	`|mkfs|dd\s+i?f?=/?dev|shutdown|reboot|poweroff|halt\b|init\s+0` +
	`|systemctl\s+(stop|disable|mask)|service\s+\S+\s+stop` +
	`|docker\s+(rm|rmi|system\s+prune)|pkill|kill(all)?\s+-9|kill\s+-KILL` +
	`|chown\s+-R|chmod\s+-R|>\s*/dev/(sd|nvme|vd|mmcblk)` +
	`|truncate\s+-s\s*0|wipefs|blkdiscard|drop\s+(table|database)`)

// IsDangerous 判断一条命令是否该标红。
func IsDangerous(cmd string) bool { return dangerRe.MatchString(cmd) }

// ---------- 存储 ----------

var uidRe = regexp.MustCompile(`^[0-9A-Za-z_.-]{1,32}$`)

// Store 读写某个账号的命令簿状态。
type Store struct{ root string }

// NewStore root 通常是 <数据目录>/users（与主机簿同目录，各自一个文件）。
func NewStore(root string) *Store {
	_ = os.MkdirAll(root, 0o700)
	return &Store{root: root}
}

func (s *Store) file(uid string) (string, error) {
	if !uidRe.MatchString(uid) {
		return "", fmt.Errorf("非法 uid: %q", uid)
	}
	d := filepath.Join(s.root, uid)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(d, "commands.json"), nil
}

// load 读状态；文件不存在或损坏时退回空状态（命令簿读不出来不该让应用不可用）。
func (s *Store) load(uid string) State {
	st := State{Meta: map[string]Meta{}}
	p, err := s.file(uid)
	if err != nil {
		return st
	}
	b, err := os.ReadFile(p)
	if err != nil || len(b) == 0 {
		return st
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return State{Meta: map[string]Meta{}}
	}
	if st.Meta == nil {
		st.Meta = map[string]Meta{}
	}
	return st
}

// save 原子写：临时文件 0600 → rename。
func (s *Store) save(uid string, st State) error {
	p, err := s.file(uid)
	if err != nil {
		return err
	}
	if st.Hidden == nil {
		st.Hidden = []string{}
	}
	if st.Custom == nil {
		st.Custom = []Item{}
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Commands 返回合并后的完整视图。
func (s *Store) Commands(uid string) Response { return s.response(s.load(uid)) }

// Upsert 新增或覆盖一条"自定义"条目。内置条目不可编辑（想改就先复制成自定义）。
func (s *Store) Upsert(uid string, in Item) (Response, error) {
	st := s.load(uid)
	in = normalizeItem(in)
	if in.ID == "" {
		in.ID = "u" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	if _, isBuiltin := builtinByID[in.ID]; isBuiltin {
		return s.response(st), nil
	}
	replaced := false
	for i := range st.Custom {
		if st.Custom[i].ID == in.ID {
			st.Custom[i] = in
			replaced = true
			break
		}
	}
	if !replaced {
		st.Custom = append(st.Custom, in)
	}
	st.Hidden = withoutString(st.Hidden, in.ID)
	if err := s.save(uid, st); err != nil {
		return Response{}, err
	}
	return s.response(st), nil
}

// Delete 内置条目 → 隐藏（可恢复）；自定义条目 → 真删。
func (s *Store) Delete(uid, id string) (Response, error) {
	st := s.load(uid)
	if _, isBuiltin := builtinByID[id]; isBuiltin {
		st.Hidden = appendUnique(st.Hidden, id)
	} else {
		out := st.Custom[:0]
		for _, c := range st.Custom {
			if c.ID != id {
				out = append(out, c)
			}
		}
		st.Custom = out
		delete(st.Meta, id)
	}
	if err := s.save(uid, st); err != nil {
		return Response{}, err
	}
	return s.response(st), nil
}

// Hide 隐藏/恢复一个内置条目（restore=true 时恢复）。
func (s *Store) Hide(uid, id string, restore bool) (Response, error) {
	if _, ok := builtinByID[id]; !ok {
		return Response{}, errors.New("只能隐藏内置命令")
	}
	st := s.load(uid)
	if restore {
		st.Hidden = withoutString(st.Hidden, id)
	} else {
		st.Hidden = appendUnique(st.Hidden, id)
	}
	if err := s.save(uid, st); err != nil {
		return Response{}, err
	}
	return s.response(st), nil
}

// Use 记一次使用（只影响排序权重，不影响命令内容）。
func (s *Store) Use(uid, id string) (Response, error) {
	st := s.load(uid)
	m := st.Meta[id]
	m.Used++
	m.LastUsedAt = time.Now().Format(time.RFC3339)
	st.Meta[id] = m
	if err := s.save(uid, st); err != nil {
		return Response{}, err
	}
	return s.response(st), nil
}

// Reset 恢复出厂：清掉隐藏记录与使用记录，自定义条目保留。
func (s *Store) Reset(uid string) (Response, error) {
	st := s.load(uid)
	st.Hidden = []string{}
	st.Meta = map[string]Meta{}
	if err := s.save(uid, st); err != nil {
		return Response{}, err
	}
	return s.response(st), nil
}

// ---------- 渲染与校验 ----------

// Lookup 在合并后的列表里找一条（也允许找已隐藏的内置条目，以便旧页面还能用）。
func (r Response) Lookup(id string) (Item, bool) {
	for _, it := range r.List {
		if it.ID == id {
			return it, true
		}
	}
	if b, ok := builtinByID[id]; ok {
		return b, true
	}
	return Item{}, false
}

// badParamRe 参数值里绝不允许出现的字符：命令拼接、命令替换、重定向。
var badParamRe = regexp.MustCompile("[;|&$`'\"<>\\\r\n]")

// Render 把模板里的 {占位符} 换成用户填的值，返回最终要写进终端的命令。
//
// 这是"从命令簿点一条 → 服务端替你敲进终端"这条路的闸门：
//   - 命令形状只能来自内置库或用户自己存的条目，前端无法凭空造一条；
//   - 参数值走白名单校验（不能含 ; | & $ ` ' " < > \ 换行，也不能以 - 开头），
//     所以即使页面被改，也拼不出额外命令 —— 只能填出模板本身的形状。
func Render(it Item, params map[string]string) (string, error) {
	cmd := it.Cmd
	for _, p := range it.Params {
		v := strings.TrimSpace(params[p.Name])
		if v == "" {
			v = strings.TrimSpace(p.Default)
		}
		if v == "" {
			return "", fmt.Errorf("请先填写「%s」", p.Name)
		}
		if err := checkParamValue(p.Name, v); err != nil {
			return "", err
		}
		cmd = strings.ReplaceAll(cmd, "{"+p.Name+"}", v)
	}
	if hasPlaceholder(cmd) {
		return "", errors.New("命令里还有没填的占位符")
	}
	return cmd, nil
}

func checkParamValue(name, v string) error {
	if len([]rune(v)) > 200 {
		return fmt.Errorf("「%s」太长了（最多 200 字）", name)
	}
	if strings.HasPrefix(v, "-") {
		return fmt.Errorf("「%s」不能以减号开头", name)
	}
	if badParamRe.MatchString(v) {
		return fmt.Errorf("「%s」里不能包含 ; | & $ ` 引号 尖括号 反斜杠 换行 这些字符", name)
	}
	return nil
}

// ---------- 内部 ----------

func (s *Store) response(st State) Response {
	hidden := make(map[string]bool, len(st.Hidden))
	for _, id := range st.Hidden {
		hidden[id] = true
	}
	list := make([]Item, 0, len(builtinItems)+len(st.Custom))
	for _, b := range builtinItems {
		if hidden[b.ID] {
			continue
		}
		b.Builtin = true
		list = append(list, withMeta(b, st))
	}
	for _, c := range st.Custom {
		c.Builtin = false
		if c.Params == nil {
			c.Params = deriveParams(c.Cmd)
		}
		list = append(list, withMeta(c, st))
	}
	return Response{List: list, Meta: st.Meta, Cats: Categories}
}

func withMeta(it Item, st State) Item {
	if m, ok := st.Meta[it.ID]; ok {
		it.Used = m.Used
		it.LastUsedAt = m.LastUsedAt
	}
	return it
}

// normalizeItem 清洗用户提交的自定义条目。
func normalizeItem(in Item) Item {
	in.Title = strings.TrimSpace(in.Title)
	in.Cmd = strings.TrimSpace(in.Cmd)
	in.Note = strings.TrimSpace(in.Note)
	in.Cat = strings.TrimSpace(in.Cat)
	if in.Cat == "" || !isKnownCat(in.Cat) {
		in.Cat = CatMine
	}
	if in.Title == "" {
		in.Title = firstLine(in.Cmd)
	}
	if in.Title == "" {
		in.Title = "未命名命令"
	}
	if len(in.Title) > 80 {
		in.Title = in.Title[:80]
	}
	if len(in.Note) > 500 {
		in.Note = in.Note[:500]
	}
	if len(in.Cmd) > 8000 {
		in.Cmd = in.Cmd[:8000]
	}
	in.Params = deriveParams(in.Cmd)
	in.Danger = IsDangerous(in.Cmd)
	in.Builtin = false
	in.Used = 0
	in.LastUsedAt = ""
	return in
}

func isKnownCat(id string) bool {
	for _, c := range Categories {
		if c.ID == id {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(strings.TrimLeft(s, "#$ "))
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func withoutString(list []string, v string) []string {
	out := list[:0]
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

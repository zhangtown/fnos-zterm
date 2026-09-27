// Package auth 从飞牛统一网关透传的请求头里认出"现在是谁"。
//
// 内嵌应用（走 /app/<app>/ 路径、监听 unix socket）不需要自己登录：飞牛桌面把请求转发过来时
// 会带上当前账号。应用要做的是把 uid 取出来做数据隔离，并按需判断管理员。
//
// 注意：**header 的确切名字要在这个环境里核实过**（/api/whoami 会回显所有收到的请求头），
// 所以这里按候选表依次尝试，并把命中来源记下来，方便排查；取不到时退回 "local"。
package auth

import (
	"errors"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
)

// Identity 是本次请求的身份判断结果。
type Identity struct {
	UID        string
	IsAdmin    bool
	AdminClaim string // 管理员标记的原始值（"1"/"true"/…），空表示网关没透传
	UIDSrc     string // uid 来自哪个请求头
	AdminSrc   string // 管理员标记来自哪个请求头
}

// ErrNotAdmin 表示按当前策略该请求没有管理员身份。
var ErrNotAdmin = errors.New("需要管理员访问权限")

var (
	uidHeaders = []string{
		"X-Trim-Userid", "X-Trim-Uid", "X-Trim-User", "X-Trim-Username",
		"X-User-Id", "X-Forwarded-User", "Remote-User",
	}
	adminHeaders = []string{
		"X-Trim-Isadmin", "X-Trim-Is-Admin", "X-Trim-Admin", "X-Forwarded-Isadmin",
	}

	// uid 会直接进文件路径，必须白名单校验（否则就是路径穿越）。
	uidRe = regexp.MustCompile(`^[0-9A-Za-z_.-]{1,32}$`)
)

// Identify 解析请求身份。任何一项取不到都只是"信息缺失"，不返回错误。
func Identify(r *http.Request) Identity {
	id := Identity{}
	for _, h := range uidHeaders {
		if v := strings.TrimSpace(r.Header.Get(h)); v != "" {
			id.UID, id.UIDSrc = v, h
			break
		}
	}
	for _, h := range adminHeaders {
		if v, ok := headerAny(r, h); ok {
			id.AdminClaim, id.AdminSrc = v, h
			id.IsAdmin = truthy(v)
			break
		}
	}
	return id
}

// headerAny 取请求头，区分"头不存在"与"头存在但为空"。
func headerAny(r *http.Request, name string) (string, bool) {
	vs, ok := r.Header[http.CanonicalHeaderKey(name)]
	if !ok || len(vs) == 0 {
		return "", false
	}
	return strings.TrimSpace(vs[0]), true
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "admin":
		return true
	}
	return false
}

// SafeUID 返回可以安全用于目录名的 uid；不合规一律回落到 "local"。
func (i Identity) SafeUID() string {
	if uidRe.MatchString(i.UID) {
		return i.UID
	}
	return "local"
}

// AdminMode 是管理员校验策略。
//
//	soft（默认）：网关给了管理员标记就必须是管理员；没给（说明头名不对或直连 socket 调试）则放行
//	strict     ：必须显式拿到真值的管理员标记，否则 403
//	off        ：完全不校验（桌面入口 allUsers=false 仍然只有管理员看得见）
func AdminMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ZTERM_ADMIN_MODE"))) {
	case "strict":
		return "strict"
	case "off":
		return "off"
	default:
		return "soft"
	}
}

var (
	warnOnce  sync.Once
	warnNever sync.Once
)

// CheckAdmin 按当前策略判断请求是否可以继续。
func CheckAdmin(r *http.Request) error {
	mode := AdminMode()
	if mode == "off" {
		return nil
	}
	id := Identify(r)
	if id.AdminClaim == "" {
		if mode == "strict" {
			return ErrNotAdmin
		}
		// 头名可能不叫 X-Trim-Isadmin：只在第一次提醒，避免刷屏。
		warnOnce.Do(func() {
			log.Printf("提示: 请求里没有管理员标记（已试 %s）；当前策略 soft 放行。请用 /api/whoami 核实实际头名后再决定是否切到 strict", strings.Join(adminHeaders, "/"))
		})
		return nil
	}
	if id.IsAdmin {
		return nil
	}
	warnNever.Do(func() {
		log.Printf("拒绝非管理员请求: %s=%q", id.AdminSrc, id.AdminClaim)
	})
	return ErrNotAdmin
}

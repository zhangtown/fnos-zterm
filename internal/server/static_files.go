package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// fileEntry 是启动时读进内存的一个静态文件。
type fileEntry struct {
	data []byte
	etag string
	mod  time.Time
	typ  string
}

// staticFiles 提供前端静态资源：启动时全部读进内存、
// 逐个算 ETag，命中 If-None-Match 直接回 304。SPA 路由回落到 index.html。
type staticFiles struct {
	source string
	mu     sync.RWMutex
	files  map[string]fileEntry
	index  fileEntry
}

var extraMIME = map[string]string{
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".html":  "text/html; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".svg":   "image/svg+xml",
	".woff2": "font/woff2",
	".woff":  "font/woff",
	".ttf":   "font/ttf",
	".ico":   "image/x-icon",
	".map":   "application/json; charset=utf-8",
	".txt":   "text/plain; charset=utf-8",
	".webmanifest": "application/manifest+json",
}

func contentType(name string) string {
	if t, ok := extraMIME[strings.ToLower(filepath.Ext(name))]; ok {
		return t
	}
	if t := mime.TypeByExtension(filepath.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

// newStaticFiles 从磁盘目录加载静态资源（开发/覆盖用）。
func newStaticFiles(dir string) (*staticFiles, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("未指定静态资源目录")
	}
	return newStaticFS(os.DirFS(dir), dir)
}

// newStaticFS 从任意 fs.FS 加载（嵌入模式用它）。
func newStaticFS(fsys fs.FS, source string) (*staticFiles, error) {
	st := &staticFiles{source: source, files: map[string]fileEntry{}}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if p != "." && (strings.HasPrefix(name, ".") || name == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		st.files["/"+p] = fileEntry{
			data: data,
			etag: `"` + hex.EncodeToString(sum[:8]) + `"`,
			mod:  time.Now(),
			typ:  contentType(p),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	idx, ok := st.files["/index.html"]
	if !ok {
		return nil, fmt.Errorf("%s 里没有 index.html", source)
	}
	st.index = idx
	return st, nil
}

// Source 返回资源来源描述（日志/诊断用）。
func (st *staticFiles) Source() string { return st.source }

func (st *staticFiles) len() int {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return len(st.files)
}

// basePlaceholder 是 index.html 里的挂载前缀占位符。飞牛把内嵌应用挂在
// /app/<应用名>/ 下，而这个前缀只有请求到达时才确定，所以由服务端注入
// <base href="/app/<应用名>/">——前端所有相对路径（./assets/…、./api/…、
// WebSocket）就都能自动对。
const basePlaceholder = "__ZTERM_BASE__"

// ServeHTTP 处理所有非 /api 请求。
func (st *staticFiles) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "只支持 GET/HEAD", http.StatusMethodNotAllowed)
		return
	}
	rel := path.Clean("/" + strings.TrimPrefix(strings.ReplaceAll(r.URL.Path, "\\", "/"), "/"))
	if rel == "/" {
		rel = "/index.html"
	}
	st.mu.RLock()
	entry, ok := st.files[rel]
	st.mu.RUnlock()
	if !ok {
		// 目录（/foo/）或前端路由：回落到 index.html；资源请求则真 404，
		// 免得把缺失的 .js 也变成一份 HTML 让浏览器报 MIME 错。
		if strings.HasPrefix(rel, "/assets/") || strings.HasPrefix(rel, "/icons/") || hasFileExt(rel) {
			http.NotFound(w, r)
			return
		}
		st.serveIndex(w, r)
		return
	}
	if rel == "/index.html" {
		st.serveIndex(w, r)
		return
	}
	st.write(w, r, entry, "public, max-age=604800, immutable")
}

// serveIndex 输出注入了挂载前缀的首页（并按内容算 ETag）。
func (st *staticFiles) serveIndex(w http.ResponseWriter, r *http.Request) {
	base := mountDir(originalPath(r))
	body := bytes.Replace(st.index.data, []byte(basePlaceholder), []byte(base), 1)
	sum := sha256.Sum256(body)
	entry := fileEntry{
		data: body,
		etag: `"` + hex.EncodeToString(sum[:8]) + `"`,
		typ:  st.index.typ,
	}
	// 首页不能缓存：升级后浏览器必须拿到新界面。
	st.write(w, r, entry, "no-cache")
}

// write 输出一个资源，并按 ETag 处理 304。
func (st *staticFiles) write(w http.ResponseWriter, r *http.Request, entry fileEntry, cacheControl string) {
	h := w.Header()
	h.Set("Content-Type", entry.typ)
	h.Set("ETag", entry.etag)
	h.Set("Cache-Control", cacheControl)
	// 防 clickjacking：终端界面被套 iframe 没有正当理由。
	h.Set("X-Frame-Options", "SAMEORIGIN")
	if match := r.Header.Get("If-None-Match"); match != "" && etagMatches(match, entry.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Length", fmt.Sprint(len(entry.data)))
	if r.Method == "HEAD" {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(entry.data)
}

// mountDir 把请求路径换算成"目录"形式，作为 <base href> 的值。
//
//	/app/zterm          → /app/zterm/
//	/app/zterm/         → /app/zterm/
//	/app/zterm/index.html → /app/zterm/
//	/                   → /
func mountDir(p string) string {
	if p == "" {
		return "/"
	}
	if strings.HasSuffix(p, "/") {
		return p
	}
	if i := strings.LastIndex(p, "/"); i >= 0 {
		if hasFileExt(p[i+1:]) {
			return p[:i+1]
		}
	}
	return p + "/"
}

func hasFileExt(p string) bool {
	base := path.Base(p)
	i := strings.LastIndex(base, ".")
	return i > 0 && i < len(base)-1
}

// etagMatches 支持逗号分隔的多值与 W/ 前缀。
func etagMatches(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || part == etag || strings.TrimPrefix(part, "W/") == etag {
			return true
		}
	}
	return false
}

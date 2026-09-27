// Package webui 把构建好的前端资源嵌进二进制。
//
// pack.sh / build.sh 会先跑 vite 生成 dist/，再 go build；这样成品包里只有一个可执行文件，
// 不会出现"界面目录没跟着过去 → 打开是白屏"的问题。
// 想在 NAS 上临时换界面，可以用 -ui <目录> 覆盖（见 server.Config.StaticDir）。
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS 返回嵌入的界面资源（已定位到 dist 根）。第二个返回值表示是否真的构建过界面。
func FS() (fs.FS, bool) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, false
	}
	return sub, true
}

package commands

import (
	"strings"
	"testing"
)

// 故意不标红的：它是"撤销动作"（取消已计划的重启），标红反而吓人。
var safeInDangerCat = map[string]bool{"danger-shutdown-cancel": true}

// 内置库的自检：这几条一旦不满足，界面上就会出现重复项、填不上或分类丢失。
func TestBuiltinLibrary(t *testing.T) {
	if len(builtinItems) < 80 {
		t.Fatalf("内置命令太少（%d 条），是不是被误删了", len(builtinItems))
	}
	seen := map[string]bool{}
	perCat := map[string]int{}
	for _, it := range builtinItems {
		if it.ID == "" {
			t.Fatalf("有条目没有 id: %q", it.Title)
		}
		if seen[it.ID] {
			t.Errorf("重复 id: %s", it.ID)
		}
		seen[it.ID] = true
		if it.Title == "" || it.Cmd == "" {
			t.Errorf("%s: 标题或命令为空", it.ID)
		}
		if !isKnownCat(it.Cat) {
			t.Errorf("%s: 未知分类 %q", it.ID, it.Cat)
		}
		if it.Cat == CatDanger && !it.Danger && !safeInDangerCat[it.ID] {
			t.Errorf("%s: 在危险分类里却没标 danger", it.ID)
		}
		perCat[it.Cat]++
		// 命令里出现的每个 {占位符} 都必须被识别成参数，否则用户填不上
		for _, m := range phRe.FindAllStringSubmatch(goTmplRe.ReplaceAllString(it.Cmd, ""), -1) {
			name := strings.TrimSpace(m[1])
			found := false
			for _, p := range it.Params {
				if p.Name == name {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: 占位符 {%s} 没有被识别成参数", it.ID, name)
			}
		}
		// 参数名必须真的出现在命令里
		for _, p := range it.Params {
			if !strings.Contains(it.Cmd, "{"+p.Name+"}") {
				t.Errorf("%s: 参数 %q 在命令里找不到", it.ID, p.Name)
			}
			if p.Kind == "" {
				t.Errorf("%s: 参数 %q 没有类型", it.ID, p.Name)
			}
		}
	}
	for _, c := range Categories {
		if c.ID == CatMine {
			continue
		}
		if perCat[c.ID] == 0 {
			t.Errorf("分类 %s(%s) 一条命令都没有", c.ID, c.Label)
		}
	}
	t.Logf("内置命令 %d 条，分布: %v", len(builtinItems), perCat)
}

// docker 的 {{.Names}} 不能被当成占位符。
func TestDeriveParamsIgnoresGoTemplates(t *testing.T) {
	got := deriveParams(`docker ps --format 'table {{.Names}}\t{{.Image}}'`)
	if len(got) != 0 {
		t.Fatalf("模板被误判成占位符: %+v", got)
	}
	got = deriveParams(`docker logs -f --tail=100 {容器}`)
	if len(got) != 1 || got[0].Name != "容器" || got[0].Kind != "container" {
		t.Fatalf("占位符解析不对: %+v", got)
	}
}

func TestIsDangerous(t *testing.T) {
	danger := []string{
		"rm -rf /vol1/1000/x", "mkfs.ext4 /dev/sda1", "systemctl stop smbd",
		"docker rm -f jellyfin", "shutdown -r now", "chmod -R 777 /vol1",
	}
	for _, c := range danger {
		if !IsDangerous(c) {
			t.Errorf("应该被判为危险: %s", c)
		}
	}
	safe := []string{
		"df -hT", "docker ps -a", "ls -lah /vol1", "systemctl status smbd",
		"ps aux | grep -i nginx", "docker logs -f --tail=100 x",
	}
	for _, c := range safe {
		if IsDangerous(c) {
			t.Errorf("不该被判为危险: %s", c)
		}
	}
}

func TestRender(t *testing.T) {
	it, ok := builtinByID["docker-logs-f"]
	if !ok {
		t.Fatal("找不到内置条目 docker-logs-f")
	}
	got, err := Render(it, map[string]string{"容器": "jellyfin"})
	if err != nil || got != "docker logs -f --tail=100 jellyfin" {
		t.Fatalf("渲染结果不对: %q %v", got, err)
	}
	// 空参数要走默认值；没默认值就报错
	if _, err := Render(it, nil); err == nil {
		t.Fatal("空参数应该报错")
	}
	// 注入尝试必须被挡住
	for _, bad := range []string{"x; rm -rf /", "x && reboot", "x`id`", "$(id)", "x|sh", "-flag"} {
		if _, err := Render(it, map[string]string{"容器": bad}); err == nil {
			t.Errorf("危险参数没被拦住: %q", bad)
		}
	}
	// 带空格的目录名是合法值
	dit, _ := builtinByID["file-cd"]
	if _, err := Render(dit, map[string]string{"目录": "/vol1/1000/我的 电影"}); err != nil {
		t.Errorf("合法参数被误拦: %v", err)
	}
}

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir + "/users")
	resp := s.Commands("1000")
	if len(resp.List) != len(builtinItems) {
		t.Fatalf("初始列表条数不对: %d != %d", len(resp.List), len(builtinItems))
	}
	// 加一条自定义
	resp, err := s.Upsert("1000", Item{Title: "看我的电影", Cmd: "ls -lah /vol1/1000/电影", Cat: CatMine})
	if err != nil {
		t.Fatalf("Upsert 失败: %v", err)
	}
	if len(resp.List) != len(builtinItems)+1 {
		t.Fatalf("新增后条数不对: %d", len(resp.List))
	}
	var mineID string
	for _, it := range resp.List {
		if !it.Builtin {
			mineID = it.ID
		}
	}
	if mineID == "" || resp.List[len(resp.List)-1].Title != "看我的电影" {
		t.Fatal("自定义条目没出现在列表里")
	}
	// 隐藏一条内置
	if _, err := s.Delete("1000", "disk-df"); err != nil {
		t.Fatalf("隐藏失败: %v", err)
	}
	resp = s.Commands("1000")
	for _, it := range resp.List {
		if it.ID == "disk-df" {
			t.Fatal("隐藏的内置条目还在列表里")
		}
	}
	// 恢复出厂 → 内置回来，自定义还在
	resp, err = s.Reset("1000")
	if err != nil {
		t.Fatalf("重置失败: %v", err)
	}
	if len(resp.List) != len(builtinItems)+1 {
		t.Fatalf("重置后条数不对: %d", len(resp.List))
	}
	// 删除自定义
	resp, err = s.Delete("1000", mineID)
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if len(resp.List) != len(builtinItems) {
		t.Fatalf("删除后条数不对: %d", len(resp.List))
	}
	// 使用计数
	resp, err = s.Use("1000", "disk-df")
	if err != nil {
		t.Fatalf("Use 失败: %v", err)
	}
	if resp.Meta["disk-df"].Used != 1 {
		t.Fatal("使用计数没记上")
	}
}

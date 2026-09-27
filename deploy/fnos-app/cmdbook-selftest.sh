#!/usr/bin/env bash
# 命令簿 API 端到端自检（在 NAS 上跑；直连 unix socket，绕过网关）
set -u
SOCK=/var/apps/zterm/target/app.sock
H=(-H "X-Trim-Isadmin:true" -H "X-Trim-Userid:1000" -H "X-Trim-Username:selfcheck" -H "Content-Type: application/json")
U="http://localhost"
j() { curl -s "${H[@]}" --unix-socket "$SOCK" "$@"; }
ok=0; bad=0
chk() { # chk 名称 期望 实际
  if [ "$2" = "$3" ]; then echo "  ✓ $1"; ok=$((ok+1)); else echo "  ✗ $1  期望[$2] 实际[$3]"; bad=$((bad+1)); fi
}

echo "== 1) 列表"
N=$(j "$U/api/commands" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(len(d["list"]))')
chk "内置+自定义条数 > 100" "yes" "$([ "$N" -gt 100 ] && echo yes)"
CATS=$(j "$U/api/commands" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(len(d["cats"]))')
chk "分类数 = 12" "12" "$CATS"
FIRST=$(j "$U/api/commands" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d["list"][0]["title"])')
echo "  第一条: $FIRST"
DANGER=$(j "$U/api/commands" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(sum(1 for x in d["list"] if x.get("danger")))')
echo "  标危险的条数: $DANGER"
WITHOPT=$(j "$U/api/commands" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(sum(1 for x in d["list"] if x.get("params") and any(p.get("kind")!="text" for p in x["params"])))')
echo "  带下拉候选的命令条数: $WITHOPT"

echo "== 2) 候选值（当场在本机查）"
for k in container service app volume dir user port image; do
  C=$(j "$U/api/commands/candidates?kind=$k" | python3 -c 'import sys,json;print(len(json.load(sys.stdin)["options"]))')
  echo "  $k: $C 项"
done

echo "== 3) 参数校验（防注入）"
IT=$(j -X POST "$U/api/commands" -d '{"title":"自检-写文件","cmd":"echo {值} > /tmp/zterm-cmdtest.txt","cat":"mine"}' \
  | python3 -c 'import sys,json;d=json.load(sys.stdin);print([x["id"] for x in d["list"] if x["title"]=="自检-写文件"][0])')
echo "  新建条目 id=$IT"
BAD=$(j -X POST "$U/api/commands/run" -d "{\"id\":\"$IT\",\"sessionId\":\"x\",\"params\":{\"值\":\"a; reboot\"}}" -o /tmp/badresp -w '%{http_code}')
chk "参数里塞 ; 被拒" "400" "$BAD"

echo "== 4) 危险命令二次确认"
DT=$(j -X POST "$U/api/commands" -d '{"title":"自检-危险","cmd":"rm -rf /tmp/zterm-cmdtest-nope","cat":"mine"}' \
  | python3 -c 'import sys,json;d=json.load(sys.stdin);print([x["id"] for x in d["list"] if x["title"]=="自检-危险"][0])')
DANGERFLAG=$(j "$U/api/commands" | python3 -c "import sys,json;d=json.load(sys.stdin);print([x.get('danger',False) for x in d['list'] if x['id']=='$DT'][0])")
chk "危险命令被自动识别" "True" "$DANGERFLAG"

echo "== 5) 会话 + 真执行"
SID=$(j -X POST "$U/api/sessions" -d '{"kind":"shell","shell":"/bin/bash","cwd":"/tmp","name":"自检-命令簿"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("id",""))')
echo "  会话 id=$SID"
if [ -n "$SID" ]; then
  NOBODY=$(j -X POST "$U/api/commands/run" -d "{\"id\":\"$DT\",\"sessionId\":\"$SID\",\"params\":{}}" -o /dev/null -w '%{http_code}')
  chk "危险命令不带 confirm → 428" "428" "$NOBODY"
  RUN=$(j -X POST "$U/api/commands/run" -d "{\"id\":\"$IT\",\"sessionId\":\"$SID\",\"params\":{\"值\":\"zterm-ok-1231\"}}" -o /dev/null -w '%{http_code}')
  chk "普通命令执行 → 200" "200" "$RUN"
  sleep 0.6
  GOT=$(sudo -n cat /tmp/zterm-cmdtest.txt 2>/dev/null || cat /tmp/zterm-cmdtest.txt 2>/dev/null)
  chk "终端里真的跑出了结果" "zterm-ok-1231" "$GOT"
  j -X DELETE "$U/api/sessions/$SID" >/dev/null
  echo "  会话已删除"
fi

echo "== 6) 隐藏 / 恢复 / 使用记录"
HID=$(j -X POST "$U/api/commands/disk-df/hide" -d '{"hidden":true}' | python3 -c 'import sys,json;d=json.load(sys.stdin);print(any(x["id"]=="disk-df" for x in d["list"]))')
chk "隐藏后列表里没了" "False" "$HID"
BACK=$(j -X POST "$U/api/commands/reset" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(any(x["id"]=="disk-df" for x in d["list"]))')
chk "恢复出厂后又回来" "True" "$BACK"
USE=$(j -X POST "$U/api/commands/disk-df/use" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d["meta"]["disk-df"]["used"])')
chk "使用计数写入" "1" "$USE"

echo "== 7) 清理自检残留"
for id in "$IT" "$DT"; do j -X DELETE "$U/api/commands/$id" >/dev/null; done
LEFT=$(j "$U/api/commands" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(sum(1 for x in d["list"] if x["title"].startswith("自检")))')
chk "自检条目已删干净" "0" "$LEFT"
sudo -n rm -f /tmp/zterm-cmdtest.txt 2>/dev/null || rm -f /tmp/zterm-cmdtest.txt 2>/dev/null || true
j -X POST "$U/api/commands/reset" >/dev/null   # 顺手把 use 记录清零

echo "== 8) 设置：命令簿执行方式"
SAVED=$(j -X POST "$U/api/settings" -d '{"cmdRunMode":"run"}' | python3 -c 'import sys,json;print(json.load(sys.stdin).get("cmdRunMode"))')
chk "cmdRunMode=run 能存" "run" "$SAVED"
RELOAD=$(j "$U/api/settings" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("cmdRunMode"))')
chk "重新读取还是 run" "run" "$RELOAD"
j -X POST "$U/api/settings" -d '{"cmdRunMode":"fill"}' >/dev/null
RELOAD2=$(j "$U/api/settings" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("cmdRunMode"))')
chk "改回 fill" "fill" "$RELOAD2"

echo "== 9) 权限：普通用户不能读命令簿"
NOPERM=$(curl -s -o /dev/null -w '%{http_code}' -H "X-Trim-Isadmin:false" -H "X-Trim-Userid:2000" --unix-socket "$SOCK" "$U/api/commands")
chk "非管理员 → 403" "403" "$NOPERM"

echo
echo "结果：$ok 通过，$bad 失败"
[ "$bad" -eq 0 ]

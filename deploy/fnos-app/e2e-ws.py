#!/usr/bin/env python3
"""zterm 端到端自检：直连 unix socket，不开浏览器就把整条链路跑一遍。
用法（从开发机）：ssh <nas> python3 - < deploy/fnos-app/e2e-ws.py
检查项：建会话 → WS 连上拿到初始画面 → 输入命令回显 → resize 生效（stty size）→
        断开重连仍能拿到滚动历史（会话保活）→ 删会话。
只有通过网关的那一跳（外部浏览器 → /app/zterm/）需要人工看一眼，其余都在这里覆盖。
"""
import base64
import json
import os
import socket
import struct
import sys
import time

SOCK = os.environ.get("ZTERM_SOCK", "/var/apps/zterm/target/app.sock")
FAIL = []


def ok(cond, label, extra=""):
    print(("  [ok]   " if cond else "  [FAIL] ") + label + (("  " + extra) if extra else ""))
    if not cond:
        FAIL.append(label)
    return cond


def http(method, path, body=None):
    s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    s.settimeout(10)
    s.connect(SOCK)
    hdr = {"Host": "localhost", "Connection": "close"}
    if body is not None:
        hdr["Content-Type"] = "application/json"
        hdr["Content-Length"] = str(len(body))
    req = f"{method} {path} HTTP/1.1\r\n" + "".join(f"{k}: {v}\r\n" for k, v in hdr.items()) + "\r\n"
    s.sendall(req.encode() + (body or b""))
    buf = b""
    while True:
        try:
            chunk = s.recv(65536)
        except socket.timeout:
            break
        if not chunk:
            break
        buf += chunk
    s.close()
    head, _, payload = buf.partition(b"\r\n\r\n")
    try:
        st = int(head.split(b" ")[1])
    except Exception:
        st = 0
    return st, payload


class WS:
    def __init__(self, path):
        self.s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.s.settimeout(5)
        self.s.connect(SOCK)
        key = base64.b64encode(os.urandom(16)).decode()
        req = (
            f"GET {path} HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\n"
            f"Connection: Upgrade\r\nSec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n"
        )
        self.s.sendall(req.encode())
        buf = b""
        while b"\r\n\r\n" not in buf:
            chunk = self.s.recv(4096)
            if not chunk:
                raise RuntimeError("WS 握手没有响应")
            buf += chunk
        head, _, self.buf = buf.partition(b"\r\n\r\n")
        self.status = int(head.split(b" ")[1])
        self.text = b""
        self.binary = b""

    def send(self, payload, opcode=1):
        b = payload if isinstance(payload, bytes) else payload.encode()
        mask = os.urandom(4)
        n = len(b)
        head = bytes([0x80 | opcode])
        if n < 126:
            head += bytes([0x80 | n])
        elif n < 65536:
            head += bytes([0x80 | 126]) + struct.pack(">H", n)
        else:
            head += bytes([0x80 | 127]) + struct.pack(">Q", n)
        self.s.sendall(head + mask + bytes(c ^ mask[i % 4] for i, c in enumerate(b)))

    def pump(self, seconds):
        end = time.time() + seconds
        while time.time() < end:
            try:
                self.s.settimeout(max(0.05, end - time.time()))
                chunk = self.s.recv(65536)
            except socket.timeout:
                break
            except OSError:
                break
            if not chunk:
                break
            self.buf += chunk
            self._parse()
        return self.text + self.binary

    def _parse(self):
        while True:
            b = self.buf
            if len(b) < 2:
                return
            op = b[0] & 0x0F
            ln = b[1] & 0x7F
            off = 2
            if ln == 126:
                if len(b) < 4:
                    return
                ln = struct.unpack(">H", b[2:4])[0]
                off = 4
            elif ln == 127:
                if len(b) < 10:
                    return
                ln = struct.unpack(">Q", b[2:10])[0]
                off = 10
            if len(b) < off + ln:
                return
            payload = b[off : off + ln]
            self.buf = b[off + ln :]
            if op == 0x9:  # ping → pong（必须回，否则会被判死链）
                self.s.sendall(bytes([0x8A, 0x80]) + os.urandom(4))
            elif op == 0x1:
                self.text += payload
            elif op in (0x2, 0x0):
                self.binary += payload

    def close(self):
        try:
            self.s.close()
        except OSError:
            pass


def main():
    print(f"zterm 自检（socket={SOCK}）")

    st, body = http("GET", "/api/health")
    ok(st == 200 and b'"ok":true' in body, "健康检查 /api/health", body[:80].decode(errors="replace"))

    st, body = http("POST", "/api/sessions", json.dumps({"kind": "shell", "cols": 100, "rows": 30}).encode())
    ok(st in (200, 201), "新建本地 shell 会话", f"HTTP {st} {body[:120].decode(errors='replace')}")
    try:
        sid = json.loads(body)["id"]
    except Exception:
        print(json.dumps(FAIL))
        return 1

    ws = WS(f"/api/sessions/{sid}/ws")
    ok(ws.status == 101, "WebSocket 升级", f"HTTP {ws.status}")
    ws.pump(2.0)
    ok(len(ws.binary) > 0, "拿到初始画面", f"{len(ws.binary)}B")

    ws.send(json.dumps({"type": "input", "data": "echo ZTERM_E2E_OK; id -un; pwd\r"}))
    ws.text = ws.binary = b""
    out = ws.pump(4.0).decode(errors="replace")
    ok("ZTERM_E2E_OK" in out, "命令输入与回显", repr(out[-160:]))

    ws.send(json.dumps({"type": "resize", "cols": 120, "rows": 40}))
    time.sleep(0.5)
    ws.send(json.dumps({"type": "input", "data": "stty size\r"}))
    ws.text = ws.binary = b""
    out = ws.pump(4.0).decode(errors="replace")
    ok("40 120" in out, "resize 生效（stty size 应为 40 120）", repr(out[-120:]))

    # 会话保活 + 滚动历史：断开再连回来，旧输出必须还在
    ws.close()
    time.sleep(1.0)
    st, body = http("GET", "/api/sessions")
    item = next((x for x in json.loads(body).get("list", []) if x.get("id") == sid), None)
    ok(item is not None and item.get("alive"), "断开期间会话仍存活", json.dumps(item, ensure_ascii=False)[:140] if item else "未找到")

    ws2 = WS(f"/api/sessions/{sid}/ws")
    ok(ws2.status == 101, "重连 WebSocket", f"HTTP {ws2.status}")
    hist = ws2.pump(3.0).decode(errors="replace")
    ok("ZTERM_E2E_OK" in hist, "重连恢复滚动历史", repr(hist[-160:]))

    st, body = http("DELETE", f"/api/sessions/{sid}")
    ok(st in (200, 204), "删除会话", f"HTTP {st} {body[:80].decode(errors='replace')}")
    ws2.close()

    print("结果：" + ("全部通过 ✅" if not FAIL else f"{len(FAIL)} 项失败 ❌ {FAIL}"))
    return 0 if not FAIL else 1


if __name__ == "__main__":
    sys.exit(main())

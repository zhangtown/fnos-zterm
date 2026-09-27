// zterm 前端主逻辑：会话列表、终端接入、主机簿、设置。
//
// 关键点：
//  1) 后端按 uid 隔离会话，前端只负责展示与输入；
//  2) 断线只影响"看"，不影响 NAS 上的进程 —— 所以重连时让服务端重放滚动历史；
//  3) 输入统一用二进制帧发送（文本帧留给 resize/ping 这些控制消息）。

import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import "./style.css";
import { api, wsURL, toast, copyText, fmtSize, timeAgo, uptime } from "./api.js";
import { MONO, THEME_DARK, THEME_LIGHT } from "./theme.js";
import { S } from "./state.js";
import { initCmds, showRail } from "./commands.js";

const $ = (id) => document.getElementById(id);

/* ---------------- 启动 ---------------- */

async function init() {
  wireStaticUI();
  try {
    const [config, system] = await Promise.all([api("api/config"), api("api/system")]);
    S.config = config || {};
    S.system = system || {};
  } catch (err) {
    fatal(err);
    return;
  }
  try {
    S.settings = (await api("api/settings")) || {};
  } catch {
    S.settings = {};
  }
  fillShellSelects();
  renderHostline();

  await refreshSessions();
  try {
    S.profiles = (await api("api/profiles")).list || [];
  } catch {
    S.profiles = [];
  }
  renderBook();
  wireDynamicUI();

  // 命令簿：它自己不碰终端，靠这两个回调把命令交给当前会话
  initCmds({
    onFill(line) {
      const sess = S.sessions.find((s) => s.id === S.cur);
      if (!sess) {
        toast("先建一个会话，命令要发到终端里", true);
        return;
      }
      if (S.wsState !== "open") toast("连接还没就绪，命令已经攒着，重连后自动发出", true);
      sendInput(line);
      if (S.term) S.term.focus();
      if (window.matchMedia("(max-width: 860px)").matches) showRail("sessions");
    },
    onRun(line) {
      sendInput(line + "\r");
      if (S.term) S.term.focus();
    },
  });

  const last = localStorage.getItem("zterm.last");
  if (last && S.sessions.some((s) => s.id === last)) {
    attachSession(last, { silent: true });
  } else {
    showEmpty();
  }
  pollHealth();
  setInterval(() => {
    if (!document.hidden && !S.fatal) refreshSessions(true);
  }, 15000);
  setInterval(pollHealth, 30000);
}

// fatal 处理"没权限 / 应用没起来"这类进不去的情况：不要留一片白屏。
function fatal(err) {
  S.fatal = true;
  const main = document.querySelector(".main");
  const code = err && err.status;
  let title = "连不上后端";
  let detail = (err && err.message) || "未知错误";
  if (code === 403) {
    title = "需要管理员账号";
    detail =
      "zterm 只对飞牛管理员开放。请用管理员账号登录飞牛桌面后重新打开本应用。若你是管理员却被拒绝，请确认应用入口未对普通用户开放。";
  } else if (code === 404) {
    title = "接口不存在";
    detail = "应用可能没有正确安装或版本不匹配，试试重新安装这个应用包。";
  }
  main.innerHTML = `
    <div class="empty">
      <h2>${title}</h2>
      <p class="hint">${escapeHTML(detail)}</p>
    </div>`;
  $("healthdot").dataset.state = "bad";
  $("healthtext").textContent = "不可用";
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function renderHostline() {
  const parts = [];
  if (S.system.hostname) parts.push(S.system.hostname);
  if (S.config.uid) parts.push("账号 " + S.config.uid);
  parts.push(S.config.version ? "zterm " + S.config.version : "");
  parts.push(S.config.embedded ? "统一网关" : "直连");
  $("hostline").textContent = parts.filter(Boolean).join(" · ");
  $("ver").textContent = S.config.version || "";
}

function fillShellSelects() {
  const shells = S.system.shells && S.system.shells.length ? S.system.shells : ["/bin/bash", "/bin/sh"];
  for (const id of ["f-shell", "s-shell"]) {
    const sel = $(id);
    if (!sel) continue;
    const keep = sel.value;
    sel.innerHTML = "";
    for (const sh of shells) {
      const o = document.createElement("option");
      o.value = sh;
      o.textContent = sh;
      sel.appendChild(o);
    }
    if (keep) sel.value = keep;
  }
  const prefer = S.settings.defaultShell;
  if (prefer && shells.includes(prefer)) {
    $("f-shell").value = prefer;
    $("s-shell").value = prefer;
  }
  $("f-cwd").value = S.settings.defaultCwd || "/";
  $("f-cwd").placeholder = S.settings.defaultCwd || "/";
}

/* ---------------- 会话列表 ---------------- */

async function refreshSessions(silent) {
  try {
    const data = await api("api/sessions");
    S.sessions = data.list || [];
  } catch (err) {
    if (!silent) toast("读取会话失败：" + err.message, true);
    return;
  }
  renderList();
  // 当前会话被别处结束了：退出态提示一下
  if (S.cur) {
    const s = S.sessions.find((x) => x.id === S.cur);
    if (s && !s.alive) setConnState("exited");
  }
}

function renderList() {
  const list = $("list");
  list.innerHTML = "";
  $("count").textContent = String(S.sessions.length);
  for (const s of S.sessions) {
    const row = document.createElement("div");
    row.className = "sess" + (s.id === S.cur ? " on" : "") + (s.alive ? "" : " dead");
    row.tabIndex = 0;
    row.dataset.id = s.id;
    const kindText = s.kind === "ssh" ? "SSH" : "本地";
    const where = s.kind === "ssh" ? s.target || "" : s.cwd || "";
    row.innerHTML = `
      <span class="dot" data-state="${s.alive ? "ok" : "exited"}"></span>
      <span class="sinfo">
        <span class="sname">${escapeHTML(s.name)}</span>
        <span class="smeta">${escapeHTML(kindText + " · " + where)}</span>
      </span>
      <button class="sclose" title="结束会话" aria-label="结束会话">
        <svg class="i" viewBox="0 0 16 16"><path d="M4.2 4.2l7.6 7.6M11.8 4.2l-7.6 7.6"/></svg>
      </button>`;
    row.addEventListener("click", (ev) => {
      if (ev.target.closest(".sclose")) return;
      attachSession(s.id);
    });
    row.addEventListener("dblclick", () => renameSession(s.id, s.name));
    row.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter" || ev.key === " ") {
        ev.preventDefault();
        attachSession(s.id);
      }
    });
    row.querySelector(".sclose").addEventListener("click", (ev) => {
      ev.stopPropagation();
      killSession(s.id, s.name);
    });
    list.appendChild(row);
  }
}

function showEmpty() {
  $("empty").hidden = false;
  $("termwrap").hidden = true;
}

function showTerm() {
  $("empty").hidden = true;
  $("termwrap").hidden = false;
  requestAnimationFrame(fitNow);
}

function setHeader(sess) {
  $("tname").textContent = sess.name || "会话";
  const bits = [];
  if (sess.kind === "ssh") bits.push("ssh " + (sess.target || ""));
  else bits.push(sess.cwd || "本地");
  bits.push("创建于 " + timeAgo(sess.createdAt));
  $("ttarget").textContent = bits.join(" · ");
  $("tsize").textContent = fmtSize(sess.cols, sess.rows);
}

function setConnState(state) {
  const dot = $("tdot");
  dot.dataset.state = state;
  const map = {
    connecting: "连接中…",
    open: "",
    reconnecting: "断线重连中…",
    exited: "已退出",
    bad: "连接失败",
    closed: "已断开",
  };
  // 状态文字放在目标行尾部，避免多一行占位
  const t = $("ttarget");
  if (map[state]) {
    t.dataset.state = state;
    if (!t.dataset.base) t.dataset.base = t.textContent;
    t.textContent = t.dataset.base + " · " + map[state];
  } else {
    if (t.dataset.base) t.textContent = t.dataset.base;
    delete t.dataset.state;
  }
}

/* ---------------- 终端 ---------------- */

function ensureTerm() {
  if (S.term) return S.term;
  const term = new Terminal({
    fontFamily: MONO,
    fontSize: clampFont(S.settings.fontSize),
    cursorBlink: S.settings.cursorBlink !== false,
    theme: S.settings.theme === "light" ? THEME_LIGHT : THEME_DARK,
    scrollback: 5000,
    rightClickSelectsWord: true,
    macOptionIsMeta: true,
    allowProposedApi: false,
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  term.loadAddon(new WebLinksAddon());
  term.open($("term"));
  term.onData((d) => sendInput(d));
  // onBinary 给的是"每个字符一个字节"的字符串，不能走 TextEncoder
  term.onBinary((d) => {
    const bytes = new Uint8Array(d.length);
    for (let i = 0; i < d.length; i++) bytes[i] = d.charCodeAt(i) & 0xff;
    sendBytes(bytes);
  });
  S.term = term;
  S.fit = fit;
  applyTermSettings();

  const ro = new ResizeObserver(() => {
    clearTimeout(ro._t);
    ro._t = setTimeout(fitNow, 120);
  });
  ro.observe($("term"));
  window.addEventListener("orientationchange", () => setTimeout(fitNow, 250));
  if (window.visualViewport) window.visualViewport.addEventListener("resize", () => setTimeout(fitNow, 120));
  return term;
}

function clampFont(n) {
  n = Number(n) || 14;
  return Math.min(32, Math.max(8, Math.round(n)));
}

function applyTermSettings() {
  if (!S.term) return;
  S.term.options.fontSize = clampFont(S.settings.fontSize);
  S.term.options.cursorBlink = S.settings.cursorBlink !== false;
  S.term.options.theme = S.settings.theme === "light" ? THEME_LIGHT : THEME_DARK;
  $("termwrap").classList.toggle("light", S.settings.theme === "light");
  requestAnimationFrame(fitNow);
}

// fitNow 重新计算行列并同步给后端（后端据此发 SIGWINCH，vim/htop 才会跟着变）。
function fitNow() {
  if (!S.term || !S.cur || $("termwrap").hidden) return;
  try {
    S.fit.fit();
  } catch {
    /* 容器还没布局好，忽略 */
  }
  const size = fmtSize(S.term.cols, S.term.rows);
  if (size !== S.lastSize) {
    S.lastSize = size;
    $("tsize").textContent = size;
    sendCtrl({ type: "resize", cols: S.term.cols, rows: S.term.rows });
  }
}

function attachSession(id, { silent = false } = {}) {
  const sess = S.sessions.find((s) => s.id === id);
  if (!sess) return;
  if (S.cur === id && S.ws && S.wsState === "open") return;

  S.cur = id;
  S.retry = 0;
  S.pending = [];
  S.pendingBytes = 0;
  S.lastSize = "";
  localStorage.setItem("zterm.last", id);

  const term = ensureTerm();
  term.reset();
  showTerm();
  setHeader(sess);
  renderList();
  if (!silent) term.focus();
  openWS(sess);
}

function detachWS() {
  S.wantClose = true;
  clearTimeout(S.retryTimer);
  stopPing();
  if (S.ws) {
    try {
      S.ws.close(1000, "detach");
    } catch {
      /* 已经关了 */
    }
    S.ws = null;
  }
  S.wsState = "idle";
}

function openWS(sess) {
  detachWS();
  S.wantClose = false;
  S.wsState = "connecting";
  setConnState(S.retry > 0 ? "reconnecting" : "connecting");

  const t = S.term;
  const cols = t.cols || 100;
  const rows = t.rows || 30;
  const url = wsURL(`api/sessions/${encodeURIComponent(sess.id)}/ws?cols=${cols}&rows=${rows}`);
  let ws;
  try {
    ws = new WebSocket(url);
  } catch (err) {
    scheduleReconnect(sess);
    return;
  }
  ws.binaryType = "arraybuffer";
  S.ws = ws;

  ws.onopen = () => {
    if (S.ws !== ws) return;
    S.wsState = "open";
    S.retry = 0;
    setConnState("open");
    flushPending();
    fitNow();
    startPing();
    t.focus();
  };

  ws.onmessage = (ev) => {
    if (S.ws !== ws) return;
    if (typeof ev.data === "string") {
      handleCtrl(ev.data, sess);
      return;
    }
    t.write(new Uint8Array(ev.data));
  };

  ws.onclose = (ev) => {
    if (S.ws !== ws) return;
    S.ws = null;
    S.wsState = "idle";
    stopPing();
    if (S.wantClose || ev.code === 1000) return;
    scheduleReconnect(sess);
  };

  ws.onerror = () => {
    /* 统一走 onclose */
  };
}

// scheduleReconnect 后端会话还活着时不断重试；浏览器关掉再回来也是这条路。
function scheduleReconnect(sess) {
  if (S.cur !== sess.id) return;
  const delays = [500, 1000, 2000, 4000, 8000, 10000];
  const wait = delays[Math.min(S.retry, delays.length - 1)];
  S.retry++;
  setConnState("reconnecting");
  clearTimeout(S.retryTimer);
  S.retryTimer = setTimeout(async () => {
    if (S.cur !== sess.id) return;
    let alive = true;
    try {
      const data = await api("api/sessions");
      S.sessions = data.list || [];
      renderList();
      const now = S.sessions.find((s) => s.id === sess.id);
      if (!now) {
        toast("会话已不存在", true);
        setConnState("exited");
        showEmpty();
        S.cur = null;
        localStorage.removeItem("zterm.last");
        return;
      }
      alive = now.alive;
      setHeader(now);
    } catch {
      // 后端暂时拿不到（应用重启/网络抖动）：继续重试
    }
    if (!alive) {
      setConnState("exited");
      S.term.write("\r\n\x1b[2m[会话已退出]\x1b[0m\r\n");
      return;
    }
    // 重连要让服务端重放历史，否则本地缓冲会和远端叠加成重复内容
    S.term.reset();
    openWS(sess);
  }, wait);
}

function startPing() {
  stopPing();
  S.pingTimer = setInterval(() => sendCtrl({ type: "ping" }), 25000);
}

function stopPing() {
  clearInterval(S.pingTimer);
  S.pingTimer = null;
}

function sendCtrl(obj) {
  if (S.ws && S.wsState === "open") {
    try {
      S.ws.send(JSON.stringify(obj));
    } catch {
      /* 下一拍重连 */
    }
  }
}

function sendInput(str) {
  sendBytes(new TextEncoder().encode(str));
}

function sendBytes(bytes) {
  if (S.ws && S.wsState === "open") {
    try {
      S.ws.send(bytes);
      return;
    } catch {
      /* 落到缓冲 */
    }
  }
  // 断线期间的输入先攒着（有上限，避免长时间离线把内存吃掉）
  if (S.pendingBytes + bytes.length > 8192) return;
  S.pending.push(bytes);
  S.pendingBytes += bytes.length;
}

function flushPending() {
  if (!S.pending.length) return;
  const all = S.pending;
  S.pending = [];
  S.pendingBytes = 0;
  for (const b of all) {
    try {
      S.ws.send(b);
    } catch {
      break;
    }
  }
}

function handleCtrl(text, sess) {
  let msg;
  try {
    msg = JSON.parse(text);
  } catch {
    return;
  }
  switch (msg.type) {
    case "ready": {
      if (msg.name) {
        sess.name = msg.name;
        setHeader({ ...sess, cols: msg.cols, rows: msg.rows });
      }
      if (!msg.alive) setConnState("exited");
      break;
    }
    case "exit": {
      setConnState("exited");
      S.term.write(`\r\n\x1b[2m[进程已退出，退出码 ${msg.code}]\x1b[0m\r\n`);
      refreshSessions(true);
      break;
    }
    default:
      break;
  }
}

/* ---------------- 会话增删 ---------------- */

async function createSession(payload) {
  try {
    const info = await api("api/sessions", { method: "POST", body: payload });
    await refreshSessions(true);
    attachSession(info.id);
    toast(`已创建 ${info.name}`);
  } catch (err) {
    toast("创建失败：" + err.message, true);
  }
}

async function killSession(id, name) {
  if (!confirm(`结束「${name}」？会话里的进程会被杀掉。`)) return;
  try {
    await api(`api/sessions/${encodeURIComponent(id)}`, { method: "DELETE" });
  } catch (err) {
    toast("结束失败：" + err.message, true);
    return;
  }
  if (S.cur === id) {
    detachWS();
    S.cur = null;
    localStorage.removeItem("zterm.last");
    showEmpty();
  }
  await refreshSessions(true);
  toast("已结束会话");
}

async function renameSession(id, old) {
  const name = prompt("会话名称", old || "");
  if (name === null) return;
  const trimmed = name.trim();
  if (!trimmed || trimmed === old) return;
  try {
    await api(`api/sessions/${encodeURIComponent(id)}`, { method: "PATCH", body: { name: trimmed } });
    await refreshSessions(true);
  } catch (err) {
    toast("改名失败：" + err.message, true);
  }
}

/* ---------------- 健康检查 ---------------- */

async function pollHealth() {
  try {
    const h = await api("api/health", { timeout: 8000 });
    $("healthdot").dataset.state = "ok";
    const running = (h.sessions && h.sessions.alive) || 0;
    $("healthtext").textContent = `已连接 · ${running} 个会话在跑 · 已运行 ${uptime(h.uptime)}`;
  } catch {
    $("healthdot").dataset.state = "bad";
    $("healthtext").textContent = "后端未响应";
  }
}

/* ---------------- 弹窗：新建会话 ---------------- */

function openNew(tab = "shell") {
  fillShellSelects();
  fillProfileSelect();
  switchTab(tab);
  const dlg = $("dlg-new");
  $("new-note").textContent = "";
  dlg.showModal();
  setTimeout(() => (tab === "ssh" ? $("f-host") : $("f-cwd")).focus(), 30);
}

function switchTab(tab) {
  for (const seg of document.querySelectorAll("#dlg-new .seg")) {
    seg.classList.toggle("on", seg.dataset.tab === tab);
  }
  for (const pane of document.querySelectorAll("#dlg-new .pane")) {
    pane.hidden = pane.dataset.pane !== tab;
  }
  $("dlg-new").dataset.tab = tab;
}

function fillProfileSelect() {
  const sel = $("f-profile");
  const keep = sel.value;
  sel.innerHTML = `<option value="">— 手动填写 —</option>`;
  for (const p of S.profiles) {
    const o = document.createElement("option");
    o.value = p.id;
    o.textContent = `${p.name}（${p.user || "root"}@${p.host}${p.port && p.port !== 22 ? ":" + p.port : ""}）`;
    sel.appendChild(o);
  }
  if (keep) sel.value = keep;
}

function submitNew() {
  const tab = $("dlg-new").dataset.tab || "shell";
  if (tab === "shell") {
    const shell = $("f-shell").value;
    const cwd = $("f-cwd").value.trim();
    const name = $("f-name").value.trim();
    createSession({ kind: "shell", shell, cwd, name, cols: 100, rows: 30 });
    return true;
  }
  const profileId = $("f-profile").value;
  const host = $("f-host").value.trim();
  const port = parseInt($("f-port").value.trim() || "0", 10) || 0;
  const user = $("f-user").value.trim();
  const extra = ($("f-extra").value.trim() ? $("f-extra").value.trim().split(/\s+/) : []);
  if (!profileId && !host) {
    $("new-note").textContent = "请填写主机地址，或从主机簿选一个";
    $("new-note").classList.add("bad");
    return false;
  }
  $("new-note").classList.remove("bad");
  createSession({ kind: "ssh", profileId, ssh: { host, port, user, extra }, cols: 100, rows: 30 });
  return true;
}

/* ---------------- 弹窗：主机簿 ---------------- */

function renderBook() {
  const box = $("booklist");
  box.innerHTML = "";
  if (!S.profiles.length) {
    const p = document.createElement("p");
    p.className = "hint";
    p.textContent = "还没有保存的主机。填在下面点保存，之后一键连接。";
    box.appendChild(p);
    return;
  }
  for (const p of S.profiles) {
    const row = document.createElement("div");
    row.className = "bookrow";
    const addr = `${p.user ? p.user + "@" : ""}${p.host}${p.port && p.port !== 22 ? ":" + p.port : ""}`;
    const extra = p.extra && p.extra.length ? " · " + p.extra.join(" ") : "";
    row.innerHTML = `
      <span class="binfo">
        <span class="bname">${escapeHTML(p.name)}</span>
        <span class="bmeta">${escapeHTML(addr + extra)}</span>
      </span>
      <span class="bactions">
        <button class="btn" data-act="go">连接</button>
        <button class="btn" data-act="edit">修改</button>
        <button class="btn" data-act="del">删除</button>
      </span>`;
    row.querySelector('[data-act="go"]').addEventListener("click", () => {
      $("dlg-book").close();
      createSession({ kind: "ssh", profileId: p.id, cols: 100, rows: 30 });
    });
    row.querySelector('[data-act="edit"]').addEventListener("click", () => {
      $("b-id").value = p.id;
      $("b-host").value = p.host;
      $("b-port").value = p.port && p.port !== 22 ? String(p.port) : "";
      $("b-user").value = p.user || "";
      $("b-name").value = p.name || "";
      $("book-note").textContent = "正在修改：" + p.name;
      $("b-host").focus();
    });
    row.querySelector('[data-act="del"]').addEventListener("click", async () => {
      if (!confirm(`从主机簿删除「${p.name}」？`)) return;
      try {
        const data = await api(`api/profiles/${encodeURIComponent(p.id)}`, { method: "DELETE" });
        S.profiles = data.list || [];
        renderBook();
        fillProfileSelect();
        toast("已删除");
      } catch (err) {
        toast("删除失败：" + err.message, true);
      }
    });
    box.appendChild(row);
  }
}

async function saveBook() {
  const host = $("b-host").value.trim();
  if (!host) {
    $("book-note").textContent = "主机地址必填";
    $("book-note").classList.add("bad");
    return;
  }
  $("book-note").classList.remove("bad");
  const payload = {
    id: $("b-id").value,
    host,
    port: parseInt($("b-port").value.trim() || "0", 10) || 0,
    user: $("b-user").value.trim(),
    name: $("b-name").value.trim(),
  };
  try {
    const data = await api("api/profiles", { method: "POST", body: payload });
    S.profiles = data.list || [];
    $("b-id").value = "";
    $("b-host").value = "";
    $("b-port").value = "";
    $("b-user").value = "";
    $("b-name").value = "";
    $("book-note").textContent = "已保存";
    renderBook();
    fillProfileSelect();
    toast("主机已保存");
  } catch (err) {
    $("book-note").textContent = err.message;
    $("book-note").classList.add("bad");
  }
}

/* ---------------- 弹窗：设置 ---------------- */

function openSettings() {
  S.settings = S.settings || {};
  $("s-font").value = String(clampFont(S.settings.fontSize));
  $("s-theme").value = S.settings.theme === "light" ? "light" : "dark";
  $("s-blink").checked = S.settings.cursorBlink !== false;
  $("s-cwd").value = S.settings.defaultCwd || "/";
  if (S.settings.defaultShell) $("s-shell").value = S.settings.defaultShell;
  $("s-runmode").value = S.settings.cmdRunMode === "run" ? "run" : "fill";
  $("s-hint").textContent =
    `会话上限 ${S.config.maxSessions || 8} 个/账号 · 滚动历史 512 KB/会话 · 数据目录 ${S.config.dataDirForUser || "-"}` +
    " · 快捷键：Alt+Shift+T 新建、Alt+Shift+W 结束、Alt+Shift+K 清屏、Alt+Shift+↑/↓ 切换、Ctrl+Shift+C/V 复制粘贴";
  $("set-note").textContent = "";
  $("dlg-settings").showModal();
}

async function saveSettings() {
  const payload = {
    fontSize: clampFont($("s-font").value),
    theme: $("s-theme").value,
    cursorBlink: $("s-blink").checked,
    defaultShell: $("s-shell").value,
    defaultCwd: $("s-cwd").value.trim() || "/",
    cmdRunMode: $("s-runmode").value === "run" ? "run" : "fill",
  };
  try {
    S.settings = await api("api/settings", { method: "POST", body: payload });
    applyTermSettings();
    $("f-cwd").placeholder = S.settings.defaultCwd || "/";
    toast("设置已保存");
    return true;
  } catch (err) {
    $("set-note").textContent = err.message;
    $("set-note").classList.add("bad");
    return false;
  }
}

/* ---------------- 粘贴 ---------------- */

function openPaste() {
  if (!S.cur) {
    toast("先选一个会话", true);
    return;
  }
  $("p-text").value = "";
  $("dlg-paste").showModal();
  setTimeout(() => $("p-text").focus(), 40);
}

/* ---------------- 交互接线 ---------------- */

function wireStaticUI() {
  $("btn-new").addEventListener("click", () => openNew("shell"));
  $("btn-new2").addEventListener("click", () => openNew("shell"));
  $("btn-ssh2").addEventListener("click", () => openNew("ssh"));
  $("btn-book").addEventListener("click", () => {
    renderBook();
    $("dlg-book").showModal();
  });
  $("btn-settings").addEventListener("click", openSettings);
}

function wireDynamicUI() {
  // 新建会话弹窗
  for (const seg of document.querySelectorAll("#dlg-new .seg")) {
    seg.addEventListener("click", () => switchTab(seg.dataset.tab));
  }
  $("f-profile").addEventListener("change", () => {
    const p = S.profiles.find((x) => x.id === $("f-profile").value);
    if (!p) return;
    $("f-host").value = p.host;
    $("f-port").value = p.port && p.port !== 22 ? String(p.port) : "";
    $("f-user").value = p.user || "";
  });
  $("form-new").addEventListener("submit", (ev) => {
    // method=dialog 的表单：submitter.value 是 ok / cancel。
    // 校验没过就 preventDefault，别让弹窗自己关掉（关掉用户就看不到提示了）。
    if (ev.submitter && ev.submitter.value === "ok") {
      ev.preventDefault();
      if (submitNew()) $("dlg-new").close();
    }
  });

  // 主机簿
  $("form-book").addEventListener("submit", (ev) => {
    if (ev.submitter && ev.submitter.value === "save") {
      ev.preventDefault();
      saveBook();
    }
  });

  // 设置
  $("form-settings").addEventListener("submit", async (ev) => {
    if (ev.submitter && ev.submitter.value === "save") {
      ev.preventDefault();
      const ok = await saveSettings();
      if (ok) $("dlg-settings").close();
    }
  });

  // 粘贴
  $("form-paste").addEventListener("submit", (ev) => {
    if (ev.submitter && ev.submitter.value === "ok") {
      ev.preventDefault();
      const text = $("p-text").value;
      if (text) {
        sendInput(text);
        toast("已发送 " + text.length + " 个字符");
      }
      $("dlg-paste").close();
    }
  });

  // 终端工具条
  $("btn-clear").addEventListener("click", () => S.term && S.term.clear());
  $("btn-copy").addEventListener("click", doCopy);
  $("btn-paste").addEventListener("click", openPaste);
  $("btn-reconnect").addEventListener("click", () => {
    const sess = S.sessions.find((s) => s.id === S.cur);
    if (!sess) return;
    S.retry = 0;
    S.term.reset();
    openWS(sess);
  });
  $("btn-close").addEventListener("click", () => {
    const sess = S.sessions.find((s) => s.id === S.cur);
    if (sess) killSession(sess.id, sess.name);
  });

  // 移动端按键条
  for (const btn of document.querySelectorAll("#touchbar .tkey")) {
    btn.addEventListener("click", () => {
      if (btn.dataset.send !== undefined) sendInput(btn.dataset.send);
      if (btn.dataset.end !== undefined) sendInput("\u001b[F");
      if (S.term) S.term.focus();
    });
  }

  // 终端区域点击聚焦
  $("term").addEventListener("click", () => S.term && S.term.focus());

  // 快捷键：Alt+Shift 组合避开 readline 占用的 Alt+字母
  window.addEventListener(
    "keydown",
    (ev) => {
      const k = ev.key;
      if (ev.altKey && ev.shiftKey) {
        if (k === "T" || k === "t") {
          ev.preventDefault();
          openNew("shell");
          return;
        }
        if (k === "W" || k === "w") {
          ev.preventDefault();
          const sess = S.sessions.find((s) => s.id === S.cur);
          if (sess) killSession(sess.id, sess.name);
          return;
        }
        if (k === "K" || k === "k") {
          ev.preventDefault();
          if (S.term) S.term.clear();
          return;
        }
        if (k === "ArrowUp" || k === "ArrowDown") {
          ev.preventDefault();
          cycleSession(k === "ArrowUp" ? -1 : 1);
          return;
        }
      }
      if (ev.altKey && /^[1-9]$/.test(k)) {
        const idx = Number(k) - 1;
        const sess = S.sessions[idx];
        if (sess) {
          ev.preventDefault();
          attachSession(sess.id);
        }
        return;
      }
      if (ev.ctrlKey && ev.shiftKey && (k === "C" || k === "c")) {
        ev.preventDefault();
        doCopy();
        return;
      }
      if ((ev.ctrlKey && ev.shiftKey && (k === "V" || k === "v")) || (ev.shiftKey && k === "Insert")) {
        if (ev.ctrlKey) {
          ev.preventDefault();
          openPaste();
        }
      }
    },
    true
  );

  // 切回页面时：可能刚从后台回来，补一次尺寸与（必要时的）重连
  document.addEventListener("visibilitychange", () => {
    if (document.hidden) return;
    fitNow();
    if (!S.cur) return;
    const sess = S.sessions.find((s) => s.id === S.cur);
    if (sess && sess.alive && (!S.ws || S.wsState === "idle")) {
      S.retry = 0;
      openWS(sess);
    }
  });
}

function cycleSession(delta) {
  if (!S.sessions.length) return;
  const ids = S.sessions.map((s) => s.id);
  let i = ids.indexOf(S.cur);
  if (i < 0) i = 0;
  const next = ids[(i + delta + ids.length) % ids.length];
  attachSession(next);
}

async function doCopy() {
  const sel = S.term ? S.term.getSelection() : "";
  if (!sel) {
    toast("先在终端里选中要复制的内容", true);
    return;
  }
  const ok = await copyText(sel);
  toast(ok ? "已复制" : "复制失败，请手动选中复制", !ok);
}

init();

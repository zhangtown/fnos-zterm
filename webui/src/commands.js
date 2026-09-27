// 命令簿：把"常用的 Linux / 飞牛运维命令"做成点一下就能用的清单。
//
// 设计取舍：
//  1) 默认**只填进终端不回车**（设置里可改成直接执行）—— 不熟命令的人先看一眼更安全；
//  2) 命令里可以写 {容器} {目录} 这种占位符，点击时弹框填；容器名/服务名/目录能直接下拉选，
//     所以不用背 `docker ps --format '{{.Names}}'` 这类东西；
//  3) 危险命令（rm -rf / mkfs / dd / systemctl stop / shutdown …）标红 + 二次确认，
//     而且内置库里它们单独放在"危险操作"分类里默认折叠；
//  4) 内置命令不能改但能"藏起来"，也能"存成我的"再改；自己加的命令随便改。
//
// 后端接口：GET/POST /api/commands、DELETE /api/commands/{id}、
//           POST /api/commands/{id}/hide、/use、POST /api/commands/reset、
//           GET /api/commands/candidates?kind=、POST /api/commands/run（直接执行那条路）。

import { api, toast } from "./api.js";
import { S } from "./state.js";

const $ = (id) => document.getElementById(id);

// 与后端 internal/commands 保持一致的宽松校验：只挡"换个意思"的元字符。
const DENY = /[;&|`$(){}<>\n\r]/;

const ICON = {
  run: '<svg class="i" viewBox="0 0 16 16"><path d="M5 3.6l7.4 4.4L5 12.4z"/></svg>',
  edit: '<svg class="i" viewBox="0 0 16 16"><path d="M11.2 2.9l1.9 1.9-7 7-2.5.6.6-2.5z"/></svg>',
  del: '<svg class="i" viewBox="0 0 16 16"><path d="M4.2 4.2l7.6 7.6M11.8 4.2l-7.6 7.6"/></svg>',
  hide: '<svg class="i" viewBox="0 0 16 16"><path d="M2.6 8s2.2-3.6 5.4-3.6S13.4 8 13.4 8s-2.2 3.6-5.4 3.6S2.6 8 2.6 8z"/><path d="M3.4 3.4l9.2 9.2"/></svg>',
  mine: '<svg class="i" viewBox="0 0 16 16"><path d="M8 2.6v10.8M2.6 8h10.8"/></svg>',
};

// 一个会话都没有时，命令得先有个地方落。
function activeSession() {
  return S.sessions.find((s) => s.id === S.cur) || null;
}

/* ---------------- 取数据 ---------------- */

async function load() {
  try {
    const resp = await api("api/commands");
    S.cmds = resp.list || [];
    S.cmdCats = resp.cats || [];
    S.cmdMeta = resp.meta || {};
  } catch (err) {
    S.cmds = [];
    S.cmdCats = [];
    if (err.status !== 403) toast("命令簿没加载出来：" + err.message, true);
  }
  render();
}

// 后端每次都回完整视图（list + meta），直接整体替换就行。
function adopt(resp) {
  if (resp && resp.list) S.cmds = resp.list;
  if (resp && resp.cats) S.cmdCats = resp.cats;
  if (resp && resp.meta) S.cmdMeta = resp.meta;
  render();
}

const candCache = new Map();

async function candidates(kind) {
  if (!kind || kind === "text") return [];
  if (candCache.has(kind)) return candCache.get(kind);
  let opts = [];
  try {
    const resp = await api("api/commands/candidates?kind=" + encodeURIComponent(kind));
    opts = resp.options || [];
  } catch {
    opts = [];
  }
  candCache.set(kind, opts);
  return opts;
}

/* ---------------- 渲染 ---------------- */

function matches(it, q) {
  if (!q) return true;
  const hay = [it.title, it.cmd, it.note, catLabel(it.cat)].join(" ").toLowerCase();
  return q
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((w) => hay.includes(w));
}

function catLabel(id) {
  const c = (S.cmdCats || []).find((x) => x.id === id);
  return c ? c.label : id;
}

function usedItems() {
  return S.cmds
    .filter((it) => (S.cmdMeta[it.id] || {}).used > 0)
    .sort((a, b) => {
      const ma = S.cmdMeta[a.id] || {};
      const mb = S.cmdMeta[b.id] || {};
      return (mb.used || 0) - (ma.used || 0) || String(mb.lastUsedAt || "").localeCompare(String(ma.lastUsedAt || ""));
    })
    .slice(0, 6);
}

function row(it, { showCat = false } = {}) {
  const el = document.createElement("div");
  el.className = "cmd" + (it.danger ? " danger" : "");
  el.tabIndex = 0;
  el.setAttribute("role", "button");
  el.dataset.id = it.id;
  el.title = it.cmd;

  const main = document.createElement("div");
  main.className = "cmdmain";
  const t = document.createElement("div");
  t.className = "cmdtitle";
  t.textContent = it.title;
  if (showCat) {
    const c = document.createElement("span");
    c.className = "cmdcat";
    c.textContent = catLabel(it.cat);
    t.appendChild(c);
  }
  const line = document.createElement("div");
  line.className = "cmdline";
  line.textContent = it.cmd;
  main.append(t, line);
  el.appendChild(main);

  const acts = document.createElement("div");
  acts.className = "cmdacts";
  const mk = (name, icon, title, fn) => {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "cmdact" + (name === "del" ? " danger" : "");
    b.title = title;
    b.innerHTML = icon;
    b.addEventListener("click", (ev) => {
      ev.stopPropagation();
      fn();
    });
    return b;
  };
  acts.appendChild(mk("run", ICON.run, "直接执行这一条（危险命令会先问）", () => use(it, { direct: true })));
  if (it.builtin) {
    acts.appendChild(mk("mine", ICON.mine, "存成我的，之后可以改", () => openAdd({ from: it })));
    acts.appendChild(mk("hide", ICON.hide, "藏起来（随时能恢复）", () => hide(it)));
  } else {
    acts.appendChild(mk("edit", ICON.edit, "修改", () => openAdd({ item: it })));
    acts.appendChild(mk("del", ICON.del, "删除", () => del(it)));
  }
  el.appendChild(acts);

  el.addEventListener("click", () => use(it));
  el.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter" || ev.key === " ") {
      ev.preventDefault();
      use(it);
    }
  });
  return el;
}

function groupHead(text, count) {
  const h = document.createElement("div");
  h.className = "cmdgroup";
  h.innerHTML = `<span>${text}</span><span class="badge">${count}</span>`;
  return h;
}

function collapseHead(label, count) {
  const b = document.createElement("button");
  b.type = "button";
  b.className = "cmdgroup collapsible";
  b.innerHTML = `<span class="caret">▸</span><span>${label}</span><span class="badge">${count}</span><span class="cwarn">默认收起</span>`;
  return b;
}

function renderCats() {
  const box = $("cmd-cats");
  if (!box) return;
  const q = S.cmdQ || "";
  const cats = [{ id: "", label: "全部" }];
  if (usedItems().length) cats.push({ id: "@used", label: "最近用过" });
  for (const c of S.cmdCats || []) {
    if (c.id === "mine") continue; // 「我的」永远在列表最后，由分组头表示
    if (!S.cmds.some((it) => it.cat === c.id)) continue;
    cats.push(c);
  }
  const counts = (id) => (id === "@used" ? usedItems().length : id === "" ? S.cmds.length : S.cmds.filter((it) => it.cat === id).length);
  box.innerHTML = "";
  for (const c of cats) {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "chip" + (S.cmdCat === c.id ? " on" : "");
    b.textContent = c.id === "" ? `${c.label} ${S.cmds.length}` : `${c.label} ${counts(c.id)}`;
    b.addEventListener("click", () => {
      S.cmdCat = c.id === S.cmdCat ? "" : c.id;
      render();
    });
    box.appendChild(b);
  }
  if (q) box.hidden = true;
  else box.hidden = false;
  $("cmd-q").value = q;
}

function render() {
  const box = $("cmd-list");
  if (!box) return;
  renderCats();
  box.innerHTML = "";

  const q = (S.cmdQ || "").trim();
  // 分类里的命令可能刚被删干净，别把一个空列表摆在那里（筛选也一并复位）
  if (S.cmdCat && S.cmdCat !== "@used" && !S.cmds.some((it) => it.cat === S.cmdCat)) S.cmdCat = "";
  let list = S.cmds.filter((it) => matches(it, q));

  // 搜索：平铺列出，最相关的（标题命中）排前面
  if (q) {
    const ql = q.toLowerCase();
    list.sort((a, b) => {
      const ra = (a.title.toLowerCase().includes(ql) ? 0 : 1) + (a.builtin ? 0 : -0.5);
      const rb = (b.title.toLowerCase().includes(ql) ? 0 : 1) + (b.builtin ? 0 : -0.5);
      return ra - rb || a.title.localeCompare(b.title, "zh");
    });
    if (!list.length) {
      box.innerHTML = `<p class="hint cmdnone">没找到。换个词试试，或点下面「＋ 加一条」自己写一条。</p>`;
      return;
    }
    box.appendChild(groupHead(`搜索“${q}”`, list.length));
    for (const it of list) box.appendChild(row(it, { showCat: true }));
    return;
  }

  if (S.cmdCat === "@used") {
    const used = usedItems();
    box.appendChild(groupHead("最近用过", used.length));
    for (const it of used) box.appendChild(row(it, { showCat: true }));
    return;
  }

  if (S.cmdCat) {
    const sub = list.filter((it) => it.cat === S.cmdCat);
    box.appendChild(groupHead(catLabel(S.cmdCat), sub.length));
    for (const it of sub) box.appendChild(row(it));
    return;
  }

  // 全部分组展示：最近用过 → 我的 → 其余分类
  const used = usedItems();
  if (used.length) {
    box.appendChild(groupHead("最近用过", used.length));
    for (const it of used) box.appendChild(row(it));
  }
  const mine = list.filter((it) => it.cat === "mine");
  if (mine.length) {
    box.appendChild(groupHead("我的", mine.length));
    for (const it of mine) box.appendChild(row(it));
  }
  for (const c of S.cmdCats || []) {
    if (c.id === "mine") continue;
    const sub = list.filter((it) => it.cat === c.id);
    if (!sub.length) continue;
    if (c.id === "danger") {
      // 危险操作默认收起 —— 想用的时候再展开
      const key = "zterm.cmddanger";
      const open = localStorage.getItem(key) === "1";
      const head = collapseHead(c.label + "（会被二次确认）", sub.length);
      head.querySelector(".caret").textContent = open ? "▾" : "▸";
      head.classList.toggle("open", open);
      head.addEventListener("click", () => {
        const next = localStorage.getItem(key) === "1" ? "0" : "1";
        localStorage.setItem(key, next);
        render();
      });
      box.appendChild(head);
      if (open) for (const it of sub) box.appendChild(row(it));
      continue;
    }
    box.appendChild(groupHead(c.label, sub.length));
    for (const it of sub) box.appendChild(row(it));
  }
}

/* ---------------- 用一条命令 ---------------- */

function hasDangerChar(v) {
  return DENY.test(v);
}

async function use(it, { direct = false } = {}) {
  if (!activeSession()) {
    toast("先建一个会话，命令要发到终端里", true);
    return;
  }
  const params = it.params || [];
  if (!params.length) {
    apply(it, {});
    return;
  }
  // 有占位符：弹框填参数（候选值当场在本机查）
  const fields = $("cu-fields");
  fields.innerHTML = "";
  $("cu-title").textContent = it.title;
  $("cu-desc").textContent = it.note || "";
  $("cu-desc").hidden = !it.note;
  $("cu-note").textContent = it.danger
    ? "这条命令有风险：执行前会再确认一次。"
    : "";
  for (const p of params) {
    const opts = p.options && p.options.length ? p.options : await candidates(p.kind);
    const lab = document.createElement("label");
    lab.className = "field";
    const span = document.createElement("span");
    span.textContent = p.label || p.name;
    const inp = document.createElement("input");
    inp.type = "text";
    inp.setAttribute("spellcheck", "false");
    inp.setAttribute("autocomplete", "off");
    inp.dataset.param = p.name;
    inp.dataset.kind = p.kind || "text";
    if (p.default) inp.value = p.default;
    else if (opts.length === 1) inp.value = opts[0];
    if (opts.length) {
      const dl = document.createElement("datalist");
      dl.id = "dl-" + it.id + "-" + p.name;
      for (const o of opts.slice(0, 200)) {
        const op = document.createElement("option");
        op.value = o;
        dl.appendChild(op);
      }
      lab.appendChild(span);
      lab.appendChild(inp);
      lab.appendChild(dl);
      inp.setAttribute("list", dl.id);
    } else {
      lab.append(span, inp);
    }
    inp.placeholder = p.kind === "dir" ? "/vol1/1000/…" : p.kind === "text" ? "填这里" : "";
    inp.addEventListener("input", () => preview(it));
    fields.appendChild(lab);
  }
  preview(it);
  $("btn-cu-fill").hidden = direct;
  $("btn-cu-run").hidden = !direct;
  $("btn-cu-copy").hidden = !it.builtin;
  $("dlg-cmduse").dataset.id = it.id;
  $("dlg-cmduse").dataset.direct = direct ? "1" : "0";
  $("dlg-cmduse").showModal();
  const first = fields.querySelector("input");
  if (first) setTimeout(() => first.focus(), 30);
}

// readParams 取当前弹框里的参数值。
function readParams() {
  const out = {};
  for (const inp of $("cu-fields").querySelectorAll("input[data-param]")) {
    out[inp.dataset.param] = inp.value.trim();
  }
  return out;
}

// build 用参数把 {占位符} 填好；参数里出现管道/; 之类直接拒绝。
function build(it, params) {
  let line = it.cmd;
  for (const p of it.params || []) {
    const v = (params && params[p.name]) ?? p.default ?? "";
    if (!v) return { err: `「${p.label || p.name}」还没填` };
    if (hasDangerChar(v)) return { err: `「${p.label || p.name}」里不能有 ; & | \` $ ( ) 这类字符` };
    line = line.split("{" + p.name + "}").join(v);
  }
  if (/\{[^{}\n]{1,24}\}/.test(line.replace(/\{\{[^{}]*\}\}/g, ""))) {
    return { err: "还有没填的占位符" };
  }
  return { line };
}

function preview(it) {
  const { line, err } = build(it, readParams());
  const el = $("cu-preview");
  el.textContent = err ? err : line;
  el.classList.toggle("bad", !!err);
}

// apply 真正把命令送到终端：fill=只填不回车，run=回车执行。
async function apply(it, params) {
  const { line, err } = build(it, params);
  if (err) {
    toast(err, true);
    return false;
  }
  const mode = S.settings.cmdRunMode === "run" ? "run" : "fill";
  const wantRun = it.__forceRun || mode === "run";
  if (wantRun && it.danger) {
    const ok = await confirmDanger(it, line);
    if (!ok) return false;
  }
  it.__forceRun = false;
  if (wantRun && S.hooks.onRun) {
    // 走服务端：它会重新校一遍参数与危险确认，不信任前端传上来的命令
    try {
      await api("api/commands/run", {
        method: "POST",
        body: { id: it.id, sessionId: S.cur, params: params || {}, confirm: true },
      });
      toast("已在终端执行");
    } catch (e) {
      // 服务端没接住就退回前端直发（本地终端不会因此不可用）
      S.hooks.onRun(line);
      toast("已发送（" + (e.message || "服务端未确认") + "）", true);
    }
  } else {
    if (S.hooks.onFill) S.hooks.onFill(line);
    toast("已填入终端，按回车执行");
  }
  bumpUse(it.id);
  return true;
}

function bumpUse(id) {
  const meta = S.cmdMeta[id] || { used: 0 };
  meta.used = (meta.used || 0) + 1;
  meta.lastUsedAt = new Date().toISOString();
  S.cmdMeta[id] = meta;
  api("api/commands/" + encodeURIComponent(id) + "/use", { method: "POST" }).catch(() => {});
}

/* ---------------- 危险确认 ---------------- */

function confirmDanger(it, line) {
  return new Promise((resolve) => {
    const dlg = $("dlg-ok");
    $("ok-title").textContent = "这条命令有风险";
    $("ok-text").textContent = `${it.title}${it.note ? " —— " + it.note : ""}。确认要在这台 NAS 上执行吗？`;
    const pre = $("ok-cmd");
    pre.textContent = line;
    pre.hidden = false;
    const form = $("form-ok");
    const done = () => {
      form.removeEventListener("submit", onSubmit);
      dlg.close();
    };
    const onSubmit = (ev) => {
      const yes = ev.submitter && ev.submitter.value === "ok";
      ev.preventDefault();
      done();
      resolve(yes);
    };
    form.addEventListener("submit", onSubmit);
    dlg.addEventListener("close", () => resolve(false), { once: true });
    dlg.showModal();
  });
}

function ask(title, text, yes = "确认", danger = true) {
  return new Promise((resolve) => {
    const dlg = $("dlg-ok");
    $("ok-title").textContent = title;
    $("ok-text").textContent = text;
    const pre = $("ok-cmd");
    pre.hidden = true;
    const btn = $("btn-ok-yes");
    btn.textContent = yes;
    btn.className = danger ? "btn btn-danger" : "btn btn-primary";
    const form = $("form-ok");
    const done = () => {
      form.removeEventListener("submit", onSubmit);
      dlg.close();
    };
    const onSubmit = (ev) => {
      const y = ev.submitter && ev.submitter.value === "ok";
      ev.preventDefault();
      done();
      resolve(y);
    };
    form.addEventListener("submit", onSubmit);
    dlg.addEventListener("close", () => resolve(false), { once: true });
    dlg.showModal();
  });
}

/* ---------------- 增删改 ---------------- */

function openAdd({ item = null, from = null } = {}) {
  const src = item || from;
  $("ca-title").textContent = item ? "修改命令" : from ? "存成我的命令" : "加一条命令";
  $("ca-id").value = item ? item.id : "";
  $("ca-name").value = src ? src.title : "";
  $("ca-cmd").value = src ? src.cmd : "";
  $("ca-note").value = src ? src.note || "" : "";
  const sel = $("ca-cat");
  sel.innerHTML = "";
  for (const c of S.cmdCats || []) {
    if (c.id === "danger") continue; // 危险分类只留给内置库
    const o = document.createElement("option");
    o.value = c.id;
    o.textContent = c.label;
    sel.appendChild(o);
  }
  sel.value = item && item.cat !== "mine" && item.cat !== "danger" ? item.cat : "mine";
  $("ca-foot").textContent = item ? "" : from ? "内置命令不受影响，这是一份副本。" : "";
  warnCheck();
  $("dlg-cmdadd").showModal();
  setTimeout(() => $("ca-name").focus(), 30);
}

function warnCheck() {
  const el = $("ca-warn");
  const cmd = $("ca-cmd").value;
  const risky = /(^|\s)(rm|dd|mkfs\w*|shutdown|reboot|poweroff)\b|>\s*\/dev\/|systemctl\s+(stop|disable|mask)|chmod\s+-R\s+777/.test(
    cmd
  );
  el.hidden = !risky;
  el.textContent = risky
    ? "看着是会改系统的命令：会标红，并在执行前二次确认（只填进终端那种用法不受影响）。"
    : "";
}

async function saveAdd() {
  const id = $("ca-id").value;
  const body = {
    id: id || undefined,
    title: $("ca-name").value.trim(),
    cmd: $("ca-cmd").value.trim(),
    cat: $("ca-cat").value || "mine",
    note: $("ca-note").value.trim(),
  };
  if (!body.cmd) {
    $("ca-foot").textContent = "命令不能空";
    $("ca-foot").classList.add("bad");
    return false;
  }
  if (!body.title) body.title = body.cmd.slice(0, 24);
  try {
    const resp = await api("api/commands", { method: "POST", body });
    adopt(resp);
    $("ca-foot").classList.remove("bad");
    $("ca-foot").textContent = "";
    toast(id ? "已保存" : "已加进命令簿");
    return true;
  } catch (err) {
    $("ca-foot").textContent = err.message;
    $("ca-foot").classList.add("bad");
    return false;
  }
}

async function del(it) {
  const ok = await ask("删除这条命令？", `「${it.title}」将被删掉，不影响其它命令。`, "删除");
  if (!ok) return;
  try {
    const resp = await api("api/commands/" + encodeURIComponent(it.id), { method: "DELETE" });
    adopt(resp);
    toast("已删除");
  } catch (err) {
    toast(err.message, true);
  }
}

async function hide(it) {
  try {
    const resp = await api("api/commands/" + encodeURIComponent(it.id) + "/hide", {
      method: "POST",
      body: { hidden: true },
    });
    adopt(resp);
    toast("已藏起来（点「恢复内置」能找回来）");
  } catch (err) {
    toast(err.message, true);
  }
}

async function restore() {
  const ok = await ask(
    "恢复内置命令？",
    "把之前藏起来的内置命令都恢复出来、清掉使用记录。你自己加的命令不受影响。",
    "恢复",
    false
  );
  if (!ok) return;
  try {
    const resp = await api("api/commands/reset", { method: "POST" });
    adopt(resp);
    toast("内置命令已恢复");
  } catch (err) {
    toast(err.message, true);
  }
}

/* ---------------- 接线 ---------------- */

function initCmds(hooks) {
  S.hooks = hooks || {};
  if (!$("cmd-list")) return;

  for (const t of document.querySelectorAll(".rtab")) {
    t.addEventListener("click", () => showRail(t.dataset.rail));
  }
  $("btn-cmds").addEventListener("click", () => {
    const on = $("pane-cmds").hidden;
    showRail(on ? "cmds" : "sessions");
    if (on) setTimeout(() => $("cmd-q").focus(), 40);
  });

  let qTimer = null;
  $("cmd-q").addEventListener("input", () => {
    clearTimeout(qTimer);
    const v = $("cmd-q").value;
    qTimer = setTimeout(() => {
      S.cmdQ = v;
      render();
    }, 120);
  });
  $("btn-cmd-add").addEventListener("click", () => openAdd({}));
  $("btn-cmd-restore").addEventListener("click", restore);

  $("form-cmduse").addEventListener("submit", async (ev) => {
    // 三个提交按钮：填进终端 / 直接执行 / 存成我的
    const v = ev.submitter ? ev.submitter.value : "cancel";
    if (v === "cancel") return;
    ev.preventDefault();
    const it = cmdByID($("dlg-cmduse").dataset.id);
    const params = readParams();
    if (!it) return;
    if (v === "ok") {
      it.__forceRun = true;
      if (await apply(it, params)) $("dlg-cmduse").close();
      it.__forceRun = false;
      return;
    }
    if (await apply(it, params)) $("dlg-cmduse").close();
  });

  $("btn-cu-copy").addEventListener("click", (ev) => {
    ev.preventDefault();
    const it = cmdByID($("dlg-cmduse").dataset.id);
    if (!it) return;
    $("dlg-cmduse").close();
    const filled = { ...it, cmd: build(it, readParams()).line || it.cmd };
    openAdd({ from: filled });
  });

  $("form-cmdadd").addEventListener("submit", async (ev) => {
    if (ev.submitter && ev.submitter.value === "save") {
      ev.preventDefault();
      if (await saveAdd()) $("dlg-cmdadd").close();
    }
  });
  $("ca-cmd").addEventListener("input", warnCheck);

  load();
  return { load, render };
}

function cmdByID(id) {
  return S.cmds.find((x) => x.id === id) || null;
}

// showRail 在「会话」「命令簿」两个侧栏页面之间切换。
function showRail(which) {
  const cmds = which === "cmds";
  $("pane-cmds").hidden = !cmds;
  $("pane-sessions").hidden = cmds;
  for (const t of document.querySelectorAll(".rtab")) t.classList.toggle("on", t.dataset.rail === which);
  document.querySelector(".rail").classList.toggle("showcmds", cmds);
}

export { initCmds, showRail, load as loadCmds };

// 与后端通信的小工具：挂载前缀换算、JSON 请求、WebSocket 地址、剪贴板、提示条。

// computeDir 从当前地址推算"应用挂载目录"（如 /app/zterm/）。
function computeDir(pathname) {
  let p = pathname || "/";
  if (p.endsWith("/")) return p;
  const last = p.slice(p.lastIndexOf("/") + 1);
  return last.includes(".") ? p.slice(0, p.lastIndexOf("/") + 1) : p + "/";
}

// mountDir 优先用服务端注入的 <base href>：应用被挂在哪个前缀下由它说了算。
export const BASE = (() => {
  const href = document.baseURI || "";
  if (href && !href.includes("__ZTERM_BASE__")) {
    try {
      const u = new URL(href);
      if (u.pathname && u.origin === location.origin) return u.pathname.endsWith("/") ? u.pathname : u.pathname + "/";
    } catch {
      /* 落到下面的兜底 */
    }
  }
  return computeDir(location.pathname);
})();

// apiURL 把 "api/xxx" 拼成带前缀的绝对路径。
export function apiURL(p) {
  return BASE + String(p).replace(/^\/+/, "");
}

// wsURL 同理，给出 WebSocket 地址。
export function wsURL(p) {
  const scheme = location.protocol === "https:" ? "wss://" : "ws://";
  return scheme + location.host + apiURL(p);
}

// api 发一个 JSON 请求；非 2xx 时抛出带后端消息的错误。
export async function api(path, { method = "GET", body, timeout = 20000 } = {}) {
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), timeout);
  try {
    const res = await fetch(apiURL(path), {
      method,
      signal: ctl.signal,
      headers: body === undefined ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
      credentials: "same-origin",
      cache: "no-store",
    });
    const text = await res.text();
    let data = null;
    if (text) {
      try {
        data = JSON.parse(text);
      } catch {
        data = { raw: text };
      }
    }
    if (!res.ok) {
      const msg = (data && (data.error || data.message)) || `请求失败（HTTP ${res.status}）`;
      const err = new Error(msg);
      err.status = res.status;
      throw err;
    }
    return data ?? {};
  } finally {
    clearTimeout(timer);
  }
}

let toastTimer = null;

// toast 在底部弹一条短提示。
export function toast(msg, bad = false) {
  const el = document.getElementById("toast");
  if (!el) return;
  el.textContent = msg;
  el.classList.toggle("bad", !!bad);
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => {
    el.hidden = true;
  }, bad ? 4200 : 2400);
}

// copyText 复制到剪贴板。飞牛桌面多是 http 访问，没有 navigator.clipboard，
// 所以退回到"临时 textarea + execCommand"这条老路（Chrome/Safari 仍可用）。
export async function copyText(text) {
  if (!text) return false;
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    /* 继续走兜底 */
  }
  try {
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    ta.style.cssText = "position:fixed;left:-9999px;top:0;opacity:0";
    document.body.appendChild(ta);
    ta.select();
    ta.setSelectionRange(0, ta.value.length);
    const ok = document.execCommand("copy");
    document.body.removeChild(ta);
    return ok;
  } catch {
    return false;
  }
}

// fmtSize 把列×行显示成 "120×34"。
export function fmtSize(cols, rows) {
  return `${cols || 0}×${rows || 0}`;
}

// timeAgo 粗略的相对时间。
export function timeAgo(ts) {
  if (!ts) return "";
  const t = new Date(ts).getTime();
  if (!t) return "";
  const s = Math.max(0, Math.round((Date.now() - t) / 1000));
  if (s < 60) return "刚刚";
  const m = Math.round(s / 60);
  if (m < 60) return `${m} 分钟前`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h} 小时前`;
  return `${Math.round(h / 24)} 天前`;
}

// uptime 把秒数显示成 "3 天 4 小时"。
export function uptime(sec) {
  if (!sec && sec !== 0) return "";
  if (sec < 60) return `${sec} 秒`;
  const m = Math.floor(sec / 60);
  if (m < 60) return `${m} 分钟`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h} 小时 ${m % 60} 分`;
  return `${Math.floor(h / 24)} 天 ${h % 24} 小时`;
}

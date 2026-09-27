// 全局运行状态。放在单独模块里，方便 main.js 与 commands.js 共享同一个实例
// （ESM 的循环引用是安全的，但状态收敛到一个模块最省事，也避免两处各有一份 S）。

export const S = {
  config: {},
  system: {},
  settings: {},
  sessions: [],
  profiles: [],
  cur: null, // 当前接入的会话 id
  term: null,
  fit: null,
  ws: null,
  wsState: "idle", // idle | connecting | open | closing
  retry: 0,
  retryTimer: null,
  pingTimer: null,
  wantClose: false,
  pending: [], // 断线期间用户敲的键，连上后补发
  pendingBytes: 0,
  lastSize: "",
  fatal: false,

  // ---- 命令簿 ----
  cmds: [], // 命令条目（内置 + 自己的）
  cmdCats: [], // 分类表（后端给，保证顺序与中文名一致）
  cmdMeta: {}, // 使用次数 / 隐藏状态
  cmdQ: "", // 搜索词
  cmdCat: "", // 当前分类筛选（""=全部，@used=最近用过）
  hooks: {}, // { onFill(line), onRun(line) } —— 由 main.js 注入，命令簿不直接碰终端
};

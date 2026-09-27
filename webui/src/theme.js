// xterm 配色。与界面令牌同一套思路：默认深色终端（界面里唯一的深色重点区），
// 强调色用比主色亮一档的 #E04B5E，保证在深底上可读。

export const MONO =
  'ui-monospace, "SFMono-Regular", "JetBrains Mono", Menlo, Consolas, "Noto Sans Mono CJK SC", monospace';

export const THEME_DARK = {
  background: "#15171c",
  foreground: "#f2f3f5",
  cursor: "#e04b5e",
  cursorAccent: "#15171c",
  selectionBackground: "rgba(224, 75, 94, 0.28)",
  black: "#2c313a",
  red: "#e04b5e",
  green: "#3e9e6a",
  yellow: "#c9a227",
  blue: "#5b8dd9",
  magenta: "#b06bc0",
  cyan: "#4ba3a3",
  white: "#d7dbe0",
  brightBlack: "#6b7280",
  brightRed: "#f0667a",
  brightGreen: "#57b87f",
  brightYellow: "#e0be4c",
  brightBlue: "#7ba7e8",
  brightMagenta: "#c88ad6",
  brightCyan: "#63bcbc",
  brightWhite: "#f2f3f5",
};

export const THEME_LIGHT = {
  background: "#ffffff",
  foreground: "#1f2328",
  cursor: "#a4243b",
  cursorAccent: "#ffffff",
  selectionBackground: "#eac6ce",
  black: "#1f2328",
  red: "#a4243b",
  green: "#2e8b57",
  yellow: "#c07a00",
  blue: "#2f5faa",
  magenta: "#8b4a9c",
  cyan: "#1f7f7f",
  white: "#5a6169",
  brightBlack: "#9aa0a8",
  brightRed: "#d5382e",
  brightGreen: "#3aa06a",
  brightYellow: "#d99a00",
  brightBlue: "#4a7fd0",
  brightMagenta: "#a866b8",
  brightCyan: "#39a0a0",
  brightWhite: "#1f2328",
};

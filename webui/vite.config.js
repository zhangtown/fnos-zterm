import { defineConfig } from "vite";

// 构建产物直接落进 internal/webui/dist，由 go:embed 打进二进制：
// 成品包里只有一个可执行文件，不会出现"界面目录没跟过去 → 白屏"。
export default defineConfig({
  // 相对路径：应用挂在 /app/zterm/ 下（前缀由服务端注入 <base href>），
  // 用 "/" 开头会让资源在网关下 404。
  base: "./",
  build: {
    outDir: "../internal/webui/dist",
    emptyOutDir: true,
    target: "es2020",
    assetsDir: "assets",
    chunkSizeWarningLimit: 1200,
    sourcemap: false,
  },
  server: {
    port: 5273,
    // 本地调界面用：先 `zterm -addr 127.0.0.1:7791` 起后端，再 npm run dev。
    proxy: {
      "/api": {
        target: process.env.ZTERM_DEV_API || "http://127.0.0.1:7791",
        changeOrigin: true,
        ws: true,
      },
    },
  },
});

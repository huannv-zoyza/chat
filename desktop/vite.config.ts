import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  // Không xoá log của Rust khi chạy `tauri dev`.
  clearScreen: false,
  server: {
    port: 1420,
    strictPort: true,
    watch: { ignored: ["**/src-tauri/**"] },
  },
  build: {
    // Một bản build dùng chung cho cả hai cách phát hành:
    //  - server Go nhúng thư mục này (web/web.go) -> mở bằng trình duyệt
    //  - Tauri đóng gói từ đây (frontendDist trong tauri.conf.json) -> app cài đặt
    outDir: "../web/ui/dist",
    emptyOutDir: true,
  },
});

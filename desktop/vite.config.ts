import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Mã bản build giao diện: server Go đọc build.json (nhúng kèm) và báo cho client qua sự kiện
// "ready"; tab nào đang chạy bản cũ sẽ hiện "Có phiên bản mới, tải lại".
const BUILD_ID = Date.now().toString(36);

export default defineConfig({
  define: { __BUILD_ID__: JSON.stringify(BUILD_ID) },
  plugins: [
    react(),
    {
      // iPhone iOS < 16.4 không hiểu regex lookbehind (?<= (?<! : chỉ một chỗ là cả trang không chạy
      // (bấm gì cũng không được). Chặn ngay lúc build.
      name: "no-regex-lookbehind",
      generateBundle(_, bundle) {
        for (const f of Object.values(bundle)) {
          if (f.type === "chunk" && /\(\?<[=!]/.test(f.code))
            this.error(`${f.fileName}: có regex lookbehind (?<= / (?<!, iPhone iOS < 16.4 sẽ không chạy được.`);
        }
      },
    },
    {
      name: "build-id",
      generateBundle() {
        this.emitFile({ type: "asset", fileName: "build.json", source: JSON.stringify({ id: BUILD_ID }) });
      },
    },
  ],
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

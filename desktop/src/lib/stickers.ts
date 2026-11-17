/**
 * Bộ sticker có sẵn trong app (tự thiết kế bằng chữ + emoji, không dùng hình có bản quyền).
 * Id dạng "<bộ>/<tên>"; server chỉ kiểm tra định dạng, nên thêm sticker mới chỉ cần sửa file này.
 */

export type TextSticker = {
  kind: "text";
  id: string;
  text: string;
  emoji: string;
  bg: string;
  fg?: string;
  tilt: number;
};
export type EmojiSticker = { kind: "emoji"; id: string; emoji: string; label: string };
export type Sticker = TextSticker | EmojiSticker;

export type StickerPack = { id: string; label: string; icon: string; stickers: Sticker[] };

const t = (name: string, text: string, emoji: string, bg: string, tilt: number, fg?: string): TextSticker => ({
  kind: "text",
  id: `text/${name}`,
  text,
  emoji,
  bg,
  fg,
  tilt,
});

const e = (name: string, emoji: string, label: string): EmojiSticker => ({
  kind: "emoji",
  id: `emo/${name}`,
  emoji,
  label,
});

export const STICKER_PACKS: StickerPack[] = [
  {
    id: "text",
    label: "Câu cửa miệng",
    icon: "💬",
    stickers: [
      t("oke", "Oke luôn", "👌", "#2f9e6b", -4),
      t("camon", "Cảm ơn nha", "🙏", "#e0559a", 3),
      t("dinh", "Đỉnh quá", "🔥", "#f06a27", -3),
      t("hong", "Hóng", "👀", "#7c5ce0", 4),
      t("xong", "Xong rồi", "✅", "#1f9aa8", -2),
      t("danglam", "Đang làm", "🛠️", "#3b7dd8", 3),
      t("doixiu", "Đợi xíu", "⏳", "#c98a12", -4),
      t("nhanroi", "Nhận rồi nhé", "📩", "#5a6acf", 2),
      t("xinloi", "Xin lỗi nha", "🥺", "#6d7f99", -3),
      t("chucmung", "Chúc mừng", "🎉", "#d99a0b", 4),
      t("goodjob", "Good job", "👏", "#2e9e4f", -2),
      t("cofen", "Cố lên", "💪", "#e8552d", 3),
      t("haha", "Haha", "😂", "#f4c430", -4, "#3a2c00"),
      t("antrua", "Ăn trưa không?", "🍜", "#d9483b", 2),
      t("cafe", "Cà phê không?", "☕", "#8a5a3c", -3),
      t("hop", "Họp không?", "📅", "#2d6fb7", 3),
      t("vethoi", "Về thôi", "🏃", "#4c5bd4", -2),
      t("khong", "Không nha", "🙅", "#b0413e", 4),
      t("yeu", "Yêu cả nhà", "❤️", "#e24a6b", -3),
      t("ngungu", "Ngủ ngon", "😴", "#38476b", 2),
    ],
  },
  {
    id: "emo",
    label: "Cảm xúc cỡ lớn",
    icon: "😎",
    stickers: [
      e("like", "👍", "Thích"),
      e("love", "❤️", "Yêu"),
      e("haha", "😂", "Haha"),
      e("cry", "😭", "Khóc"),
      e("wow", "😮", "Wow"),
      e("angry", "😡", "Giận"),
      e("party", "🥳", "Tiệc"),
      e("fire", "🔥", "Cháy"),
      e("pray", "🙏", "Cầu mong"),
      e("clap", "👏", "Vỗ tay"),
      e("cool", "😎", "Ngầu"),
      e("think", "🤔", "Suy nghĩ"),
      e("sleep", "😴", "Buồn ngủ"),
      e("mindblown", "🤯", "Sốc"),
      e("salute", "🫡", "Rõ"),
      e("hundred", "💯", "100 điểm"),
      e("coffee", "☕", "Cà phê"),
      e("tada", "🎉", "Chúc mừng"),
      e("rocket", "🚀", "Lên luôn"),
      e("heart-hands", "🫶", "Thương"),
    ],
  },
];

const BY_ID = new Map<string, Sticker>(STICKER_PACKS.flatMap((p) => p.stickers.map((s) => [s.id, s] as const)));

export function findSticker(id: string): Sticker | undefined {
  return BY_ID.get(id);
}

export function stickerLabel(s: Sticker): string {
  return s.kind === "text" ? s.text : s.label;
}

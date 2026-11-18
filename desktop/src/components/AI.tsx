import { useEffect, useRef, useState, type FormEvent } from "react";
import { Markdown } from "../lib/markdown";
import { useChat } from "../state/ChatProvider";
import { Dialog, FormError, Icon } from "./ui";

/** Tài khoản bot (người dùng không đăng ký được tên 2 ký tự nên "ai" chỉ có thể là bot). */
export const AI_USERNAME = "ai";
/** Nội dung tạm của bot khi đang nghĩ (khớp aiPending bên Go). */
const AI_PENDING = "…";

export const isAIUser = (u: { username?: string } | undefined) => u?.username === AI_USERNAME;

/** Nội dung tin của Trợ lý AI: Markdown, hoặc "đang suy nghĩ" kèm việc đang làm (đọc tin, xem ảnh, tìm web…). */
export function AIText({ content }: { content: string }) {
  if (content === AI_PENDING || content.startsWith(AI_PENDING + "\n")) {
    const step = content.slice(AI_PENDING.length).trim();
    return (
      <p className="ai-thinking" role="status">
        <span className="ai-dots" aria-hidden>
          <i />
          <i />
          <i />
        </span>
        {step || "Đang suy nghĩ…"}
      </p>
    );
  }
  return <Markdown text={content} />;
}

const SHAPES = [
  { id: "", label: "Vuông", hint: "1:1", box: "square" },
  { id: " --ngang", label: "Ngang", hint: "3:2", box: "wide" },
  { id: " --doc", label: "Dọc", hint: "2:3", box: "tall" },
] as const;

const IDEAS = [
  "Chú mèo phi hành gia lơ lửng giữa trạm vũ trụ, phong cách màu nước",
  "Phố cổ Hội An về đêm với đèn lồng, ảnh chụp điện ảnh",
  "Logo tối giản cho đội dev tên “Bug Hunters”, nền trắng",
  "Bát phở bò bốc khói trên bàn gỗ, ánh sáng buổi sáng",
];

/** Gợi ý khi có ảnh gốc: vẽ lại / đổi phong cách nhưng giữ nhân vật. */
const EDIT_IDEAS = [
  "Biến thành nhân vật hoạt hình 3D dễ thương, giữ nguyên khuôn mặt",
  "Vẽ lại theo phong cách anime, nền hoàng hôn",
  "Biến thành siêu anh hùng mặc áo choàng, nền thành phố về đêm",
  "Phong cách tranh màu nước, tông màu ấm",
];

const MAX_SOURCE = 8 * 1024 * 1024;

/**
 * Tạo ảnh bằng AI: gửi một tin "@ai vẽ …" vào kênh (kèm ảnh gốc nếu có), bot vẽ xong sẽ gửi ảnh vào đó.
 * canEdit: máy chủ có dịch vụ vẽ lại từ ảnh gốc.
 */
export function AIImageDialog({
  channelId,
  parentId,
  canEdit,
  onClose,
}: {
  channelId: number;
  parentId?: number;
  canEdit: boolean;
  onClose: () => void;
}) {
  const { sendMessage } = useChat();
  const [prompt, setPrompt] = useState("");
  const [shape, setShape] = useState<string>("");
  const [source, setSource] = useState<File | null>(null);
  const [preview, setPreview] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!source) return setPreview(null);
    const url = URL.createObjectURL(source);
    setPreview(url);
    return () => URL.revokeObjectURL(url);
  }, [source]);

  const pickSource = (f: File | undefined) => {
    setError(null);
    if (!f) return;
    if (!/^image\/(png|jpeg|webp)$/.test(f.type)) return setError("Ảnh gốc cần là PNG, JPG hoặc WebP.");
    if (f.size > MAX_SOURCE) return setError("Ảnh gốc tối đa 8 MB.");
    setSource(f);
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const p = prompt.trim().replace(/\s+/g, " ");
    if (!p) return setError(source ? "Hãy nói AI muốn vẽ lại ảnh thế nào." : "Hãy mô tả bức ảnh bạn muốn.");
    if (p.length > 800) return setError("Mô tả dài tối đa 800 ký tự.");
    sendMessage(channelId, `@ai vẽ ${p}${shape}`, source ? [source] : undefined, parentId);
    onClose();
  };

  return (
    <Dialog title="Tạo ảnh bằng AI" onClose={onClose}>
      <form className="dialog-form ai-image-form" onSubmit={submit}>
        {canEdit && (
          <div className="field">
            <span>Ảnh gốc (không bắt buộc)</span>
            {source && preview ? (
              <div className="ai-source">
                <img src={preview} alt="Ảnh gốc" />
                <div>
                  <b>{source.name}</b>
                  <small>AI sẽ vẽ lại ảnh này theo mô tả bên dưới.</small>
                  <button type="button" className="link-btn" onClick={() => setSource(null)}>
                    Bỏ ảnh gốc
                  </button>
                </div>
              </div>
            ) : (
              <button type="button" className="ai-source-pick" onClick={() => fileRef.current?.click()}>
                <Icon name="camera" size={20} />
                <span>
                  Chọn ảnh để vẽ lại
                  <small>
                    Đổi phong cách, biến thành nhân vật hoạt hình, thêm chi tiết… PNG, JPG, WebP tối đa 8 MB.
                  </small>
                </span>
              </button>
            )}
            <input
              ref={fileRef}
              type="file"
              accept="image/png,image/jpeg,image/webp"
              hidden
              onChange={(e) => {
                pickSource(e.target.files?.[0]);
                e.target.value = "";
              }}
            />
          </div>
        )}
        <label className="field">
          <span>{source ? "Muốn vẽ lại thế nào?" : "Mô tả bức ảnh"}</span>
          <textarea
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) submit(e);
            }}
            rows={4}
            maxLength={800}
            placeholder={
              source
                ? "Ví dụ: biến thành nhân vật hoạt hình 3D cầm kiếm ánh sáng, giữ nguyên khuôn mặt"
                : "Ví dụ: chú chó corgi đội mũ bảo hiểm chạy xe máy trên phố Sài Gòn, tranh vẽ tay"
            }
            autoFocus
          />
          <small>
            Viết tiếng Việt thoải mái, AI tự dịch sang mô tả cho máy vẽ. Càng cụ thể (vật, bối cảnh, phong cách) ảnh
            càng đúng ý.
          </small>
        </label>
        <div className="ai-ideas" aria-label="Gợi ý">
          {(source ? EDIT_IDEAS : IDEAS).map((idea) => (
            <button key={idea} type="button" className="ai-idea" onClick={() => setPrompt(idea)}>
              {idea}
            </button>
          ))}
        </div>
        <fieldset className="field ai-shapes">
          <legend>Khung ảnh</legend>
          {SHAPES.map((s) => (
            <label key={s.id} className={`ai-shape${shape === s.id ? " is-on" : ""}`}>
              <input type="radio" name="ai-shape" checked={shape === s.id} onChange={() => setShape(s.id)} />
              <i className={`ai-shape-box is-${s.box}`} />
              <span>
                {s.id === "" && source ? "Như ảnh gốc" : s.label}
                <small>{s.id === "" && source ? "giữ tỉ lệ" : s.hint}</small>
              </span>
            </label>
          ))}
        </fieldset>
        <FormError>{error}</FormError>
        <div className="dialog-actions">
          <button type="button" className="btn" onClick={onClose}>
            Huỷ
          </button>
          <button className="btn btn-primary">{source ? "Vẽ lại" : "Tạo ảnh"}</button>
        </div>
        <p className="ai-note">
          Ảnh {source ? "gốc và ảnh mới " : ""}được gửi vào kênh, mọi người cùng thấy. Thường mất 10–40 giây.
          {source ? " Ảnh gốc sẽ được gửi tới dịch vụ AI để vẽ lại." : ""}
        </p>
      </form>
    </Dialog>
  );
}

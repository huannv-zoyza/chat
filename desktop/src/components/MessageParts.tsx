import { Fragment, useEffect, useMemo, useRef, type ReactNode } from "react";
import { type Attachment, isInlineImage, type PublicUser } from "../lib/api";
import { displayName } from "../lib/format";
import { openExternal } from "../lib/notify";
import { isTauri } from "../lib/platform";
import { useChat } from "../state/ChatProvider";
import { Icon } from "./ui";
import { ExternalLink } from "./LinkPreview";
import { splitLinks } from "../lib/links";

/* ---------- Nội dung tin: tô màu @nhắc tên ---------- */

// Cùng quy tắc với server (controller/mentions.go).
const MENTION_RE = /(^|[^\p{L}\p{N}_.@])@([a-z0-9_.]{3,32})/giu;

/**
 * Hiển thị nội dung tin: link bấm được, "@username" của thành viên có thật đổi thành nhãn
 * tên hiển thị (nhắc tới mình hoặc @all được tô nổi hơn).
 */
export function MessageText({ content, meId, edited }: { content: string; meId: number; edited?: string }) {
  const { state } = useChat();
  const byUsername = useMemo(() => {
    const m = new Map<string, PublicUser>();
    for (const u of Object.values(state.users)) m.set(u.username, u);
    return m;
  }, [state.users]);

  // Đổi @username trong một đoạn chữ (không chứa link) thành nhãn.
  const withMentions = (text: string, keyBase: number): ReactNode[] => {
    const parts: ReactNode[] = [];
    let last = 0;
    for (const match of text.matchAll(MENTION_RE)) {
      const [, prefix, rawName] = match;
      const name = rawName.toLowerCase();
      // "@minh." ở cuối câu: thử cả có lẫn không có dấu chấm cuối
      const trimmed = name.replace(/\.+$/, "");
      const user = byUsername.get(name) ?? byUsername.get(trimmed);
      const isAll = trimmed === "all";
      if (!user && !isAll) continue;

      const start = match.index! + prefix.length;
      const usedLen = 1 + (user ? (byUsername.has(name) ? name.length : trimmed.length) : 3);
      parts.push(text.slice(last, start));
      parts.push(
        <span
          key={keyBase + start}
          className={`mention${isAll || user?.id === meId ? " is-me" : ""}`}
          title={isAll ? "Nhắc cả kênh" : `@${user!.username}`}
        >
          @{isAll ? "all" : displayName(user)}
        </span>,
      );
      last = start + usedLen;
    }
    parts.push(text.slice(last));
    return parts;
  };

  const out: ReactNode[] = [];
  let offset = 0;
  for (const part of splitLinks(content)) {
    if (part.kind === "link") {
      out.push(
        <ExternalLink key={`l${offset}`} href={part.url} className="msg-link">
          {part.url}
        </ExternalLink>,
      );
      offset += part.url.length;
    } else {
      out.push(...withMentions(part.text, offset * 1000));
      offset += part.text.length;
    }
  }

  return (
    <p className="msg-body">
      {out.map((p, i) => (
        <Fragment key={i}>{p}</Fragment>
      ))}
      {edited && (
        <span className="msg-edited" title={`Đã sửa lúc ${new Date(edited).toLocaleString("vi-VN")}`}>
          (đã sửa)
        </span>
      )}
    </p>
  );
}

/* ---------- File đính kèm ---------- */

export function formatSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(bytes < 10 * 1024 ? 1 : 0)} KB`;
  const mb = bytes / 1024 / 1024;
  return `${Number.isInteger(mb) ? mb : mb.toFixed(1)} MB`;
}

const ext = (name: string) => {
  const i = name.lastIndexOf(".");
  return i > 0
    ? name
        .slice(i + 1)
        .toUpperCase()
        .slice(0, 5)
    : "FILE";
};

/** Kích thước hiển thị của ảnh đơn: tối đa 360×300, giữ tỉ lệ (tránh nhảy cuộn khi ảnh tải xong). */
function fitSize(w: number, h: number) {
  if (!w || !h) return undefined;
  const scale = Math.min(1, 360 / w, 300 / h);
  return { width: Math.round(w * scale), height: Math.round(h * scale) };
}

export function download(url: string, fileName: string) {
  // File đã giải mã (blob:) chỉ tồn tại trong app: lưu thẳng, không mở trình duyệt ngoài.
  if (isTauri && !url.startsWith("blob:")) {
    // Webview của app không tự lưu file: nhờ trình duyệt mặc định tải về.
    void openExternal(url);
    return;
  }
  const a = document.createElement("a");
  a.href = url;
  a.download = fileName;
  a.rel = "noopener";
  document.body.appendChild(a);
  a.click();
  a.remove();
}

type AttachmentsProps = {
  attachments: Attachment[];
  onOpenImage: (a: Attachment) => void;
};

export function Attachments({ attachments, onOpenImage }: AttachmentsProps) {
  const { api } = useChat();
  const images = attachments.filter(isInlineImage);
  const isVideo = (a: Attachment) => /^video\/(mp4|webm|ogg|quicktime)$/.test(a.content_type);
  const isAudio = (a: Attachment) => /^audio\//.test(a.content_type);
  const media = attachments.filter((a) => isVideo(a) || isAudio(a));
  const files = attachments.filter((a) => !isInlineImage(a) && !media.includes(a));
  const single = images.length === 1 ? fitSize(images[0].width, images[0].height) : undefined;

  return (
    <div className="attachments">
      {images.length > 0 && (
        <div className={`image-grid${images.length === 1 ? " is-single" : ""}`}>
          {images.map((a) => (
            <button
              key={a.id}
              type="button"
              className="image-thumb"
              onClick={() => onOpenImage(a)}
              title={`${a.file_name}, bấm để xem lớn`}
              style={single}
            >
              <img
                src={api.fileUrl(a)}
                alt={a.file_name}
                loading="lazy"
                width={single?.width}
                height={single?.height}
              />
            </button>
          ))}
        </div>
      )}
      {media.map((a) =>
        isVideo(a) ? (
          <div key={a.id} className="media-attachment">
            <video className="media-player media-video" src={api.fileUrl(a)} controls preload="metadata" playsInline />
            <MediaCaption
              name={a.file_name}
              size={a.size}
              onDownload={() => download(api.fileUrl(a, true), a.file_name)}
            />
          </div>
        ) : (
          <div key={a.id} className="media-attachment">
            <MediaCaption
              name={a.file_name}
              size={a.size}
              onDownload={() => download(api.fileUrl(a, true), a.file_name)}
            />
            <audio className="media-player media-audio" src={api.fileUrl(a)} controls preload="metadata" />
          </div>
        ),
      )}
      {files.map((a) => (
        <FileCard
          key={a.id}
          name={a.file_name}
          size={a.size}
          onDownload={() => download(api.fileUrl(a, true), a.file_name)}
        />
      ))}
    </div>
  );
}

function MediaCaption({ name, size, onDownload }: { name: string; size: number; onDownload: () => void }) {
  return (
    <div className="media-caption">
      <span className="file-name" title={name}>
        {name}
      </span>
      <span className="file-size">{formatSize(size)}</span>
      <button type="button" className="icon-btn" onClick={onDownload} title="Tải xuống" aria-label={`Tải ${name}`}>
        <Icon name="download" size={16} />
      </button>
    </div>
  );
}

export function FileCard({
  name,
  size,
  onDownload,
  onRemove,
}: {
  name: string;
  size: number;
  onDownload?: () => void;
  onRemove?: () => void;
}) {
  return (
    <div className="file-card">
      <span className="file-icon" aria-hidden>
        <Icon name="file" size={20} />
        <span className="file-ext">{ext(name)}</span>
      </span>
      <span className="file-text">
        <span className="file-name" title={name}>
          {name}
        </span>
        <span className="file-size">{formatSize(size)}</span>
      </span>
      {onDownload && (
        <button type="button" className="icon-btn" onClick={onDownload} title="Tải xuống" aria-label={`Tải ${name}`}>
          <Icon name="download" />
        </button>
      )}
      {onRemove && (
        <button type="button" className="icon-btn" onClick={onRemove} title="Bỏ file" aria-label={`Bỏ ${name}`}>
          <Icon name="x" />
        </button>
      )}
    </div>
  );
}

/** Ảnh/file của tin đang gửi: xem trước từ máy, chưa có trên server. */
export function LocalFiles({ files, progress }: { files: File[]; progress?: number }) {
  const urls = useObjectUrls(files);
  // đang tải lên: ảnh mờ đi, thanh tiến trình chạy dưới đáy ảnh
  const bar =
    progress !== undefined ? (
      <span className="thumb-progress" aria-hidden>
        <span style={{ width: `${Math.max(4, Math.round(progress * 100))}%` }} />
      </span>
    ) : null;
  const images = files.map((f, i) => ({ f, url: urls[i] })).filter((x) => x.url);
  const others = files.filter((_, i) => !urls[i]);
  return (
    <div className="attachments">
      {images.length > 0 && (
        <div className={`image-grid${images.length === 1 ? " is-single" : ""}`}>
          {images.map(({ f, url }) => (
            <span key={url} className={`image-thumb is-local${progress !== undefined ? " is-uploading" : ""}`}>
              <img src={url!} alt={f.name} />
              {bar}
            </span>
          ))}
        </div>
      )}
      {others.map((f, i) => (
        <FileCard key={i} name={f.name} size={f.size} />
      ))}
    </div>
  );
}

/** Object URL để xem trước ảnh chọn từ máy; tự thu hồi khi không dùng nữa. */
export function useObjectUrls(files: File[]): (string | null)[] {
  const urls = useMemo(
    () => files.map((f) => (/^image\/(png|jpeg|gif|webp)$/.test(f.type) ? URL.createObjectURL(f) : null)),
    [files],
  );
  useEffect(() => () => urls.forEach((u) => u && URL.revokeObjectURL(u)), [urls]);
  return urls;
}

/* ---------- Xem ảnh lớn ---------- */

export function ImageViewer({ attachment, onClose }: { attachment: Attachment; onClose: () => void }) {
  const { api } = useChat();
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const d = ref.current;
    if (d && !d.open) d.showModal();
  }, []);

  return (
    <dialog
      ref={ref}
      className="viewer"
      aria-label={attachment.file_name}
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
      onClick={(e) => {
        if (e.target === ref.current || (e.target as HTMLElement).classList.contains("viewer-stage")) onClose();
      }}
    >
      <div className="viewer-bar">
        <span className="viewer-name">
          {attachment.file_name}
          <span className="viewer-meta">
            {attachment.width > 0 && `${attachment.width}×${attachment.height} · `}
            {formatSize(attachment.size)}
          </span>
        </span>
        <button
          type="button"
          className="viewer-btn"
          onClick={() => download(api.fileUrl(attachment, true), attachment.file_name)}
          aria-label="Tải xuống"
        >
          <Icon name="download" size={16} />
          <span className="btn-label">Tải xuống</span>
        </button>
        {!attachment.url.startsWith("blob:") && (
          <button
            type="button"
            className="viewer-btn"
            onClick={() => void openExternal(api.fileUrl(attachment))}
            aria-label={isTauri ? "Mở bằng trình duyệt" : "Mở tab mới"}
          >
            <Icon name="external" size={16} />
            <span className="btn-label">{isTauri ? "Mở bằng trình duyệt" : "Mở tab mới"}</span>
          </button>
        )}
        <button type="button" className="viewer-btn viewer-close" onClick={onClose} aria-label="Đóng">
          <Icon name="x" />
        </button>
      </div>
      <div className="viewer-stage">
        <img src={api.fileUrl(attachment)} alt={attachment.file_name} />
      </div>
    </dialog>
  );
}

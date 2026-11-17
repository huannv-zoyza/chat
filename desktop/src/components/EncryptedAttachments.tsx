import { useEffect, useState } from "react";
import type { Attachment } from "../lib/api";
import { e2ee, type EncFile } from "../lib/e2ee";
import { useChat } from "../state/ChatProvider";
import { Attachments, formatSize } from "./MessageParts";
import { Icon } from "./ui";

// Đã giải mã thì giữ blob URL cho cả phiên (cuộn qua lại không phải tải + giải mã lại).
const decrypted = new Map<string, Promise<string>>();

function decryptToUrl(url: string, fetchUrl: string, meta: EncFile): Promise<string> {
  const key = `${url}#${meta.k}`;
  let p = decrypted.get(key);
  if (!p) {
    p = fetch(fetchUrl)
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.arrayBuffer();
      })
      .then((buf) => e2ee.decryptFile(buf, meta))
      .then((blob) => URL.createObjectURL(blob));
    p.catch(() => decrypted.delete(key));
    decrypted.set(key, p);
  }
  return p;
}

type Props = {
  /** Thông tin thật của file (trong tin đã giải mã), cùng thứ tự với attachments. */
  files: EncFile[];
  /** File nhị phân đã mã hoá trên server. */
  attachments: Attachment[];
  onOpenImage: (a: Attachment) => void;
};

/** File/ảnh trong tin mã hoá đầu cuối: tải bản mã về, giải mã trên máy rồi hiển thị như thường. */
export function EncryptedAttachments({ files, attachments, onOpenImage }: Props) {
  const { api } = useChat();
  const [ready, setReady] = useState<(Attachment | "error" | null)[]>(() => files.map(() => null));

  useEffect(() => {
    let alive = true;
    files.forEach((meta, i) => {
      const att = attachments[i];
      if (!att) return;
      decryptToUrl(att.url, api.fileUrl(att, true), meta)
        .then((blobUrl) => {
          if (!alive) return;
          const plain: Attachment = {
            ...att,
            url: blobUrl,
            file_name: meta.name,
            content_type: meta.type,
            size: meta.size,
            width: meta.w ?? 0,
            height: meta.h ?? 0,
          };
          setReady((r) => r.map((x, j) => (j === i ? plain : x)));
        })
        .catch(() => alive && setReady((r) => r.map((x, j) => (j === i ? "error" : x))));
    });
    return () => {
      alive = false;
    };
  }, [api, files, attachments]);

  const done = ready.filter((x): x is Attachment => !!x && x !== "error");
  return (
    <>
      {done.length > 0 && <Attachments attachments={done} onOpenImage={onOpenImage} />}
      {files.map((meta, i) =>
        ready[i] === null ? (
          <div key={i} className="enc-file is-loading">
            <Icon name="lock" size={15} />
            Đang giải mã {meta.name} ({formatSize(meta.size)})…
          </div>
        ) : ready[i] === "error" ? (
          <div key={i} className="enc-file is-error">
            <Icon name="lock" size={15} />
            Không tải hoặc giải mã được {meta.name}.
          </div>
        ) : null,
      )}
    </>
  );
}

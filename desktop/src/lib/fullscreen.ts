/**
 * Phóng to toàn màn hình một khung (ô video trong cuộc gọi). iPhone không cho phóng to phần tử
 * tuỳ ý, chỉ cho chính thẻ <video> vào trình phát gốc (webkitEnterFullscreen), nên dùng cách đó.
 */

type WebkitVideo = HTMLVideoElement & { webkitEnterFullscreen?: () => void };
type WebkitElement = HTMLElement & { webkitRequestFullscreen?: () => void };
type WebkitDocument = Document & { webkitFullscreenElement?: Element | null; webkitExitFullscreen?: () => void };

export function fullscreenElement(): Element | null {
  const d = document as WebkitDocument;
  return d.fullscreenElement ?? d.webkitFullscreenElement ?? null;
}

/** Bật / tắt toàn màn hình cho el (video: thẻ video bên trong, dùng khi trình duyệt chỉ cho video). */
export function toggleFullscreen(el: HTMLElement, video?: HTMLVideoElement | null) {
  const d = document as WebkitDocument;
  if (fullscreenElement()) {
    if (d.exitFullscreen) void d.exitFullscreen().catch(() => {});
    else d.webkitExitFullscreen?.();
    return;
  }
  const e = el as WebkitElement;
  if (e.requestFullscreen) {
    void e.requestFullscreen().catch(() => (video as WebkitVideo | null)?.webkitEnterFullscreen?.());
  } else if (e.webkitRequestFullscreen) {
    e.webkitRequestFullscreen();
  } else {
    (video as WebkitVideo | null)?.webkitEnterFullscreen?.();
  }
}

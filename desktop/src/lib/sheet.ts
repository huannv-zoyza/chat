import { useEffect, useRef, type RefObject } from "react";

/** Vùng nắm kéo ở đầu bảng (thanh nắm + tiêu đề), tính bằng px từ mép trên. */
const GRAB_ZONE = 64;
/** Kéo xuống quá chừng này (px) thì đóng. */
const CLOSE_DISTANCE = 96;
/** Hoặc vuốt nhanh hơn chừng này (px/ms) thì đóng. */
const CLOSE_VELOCITY = 0.6;

/**
 * Điện thoại: hộp thoại hiện thành bảng trượt từ cạnh dưới. Cho vuốt từ phần đầu bảng xuống
 * để đóng, như bảng của iOS/Android. Màn rộng (bảng là hộp thoại giữa màn hình) thì không làm gì.
 */
export function useSheetDrag(ref: RefObject<HTMLElement | null>, onClose: () => void) {
  // giữ hàm đóng mới nhất mà không gắn lại bộ nghe mỗi lần render
  const close = useRef(onClose);
  useEffect(() => {
    close.current = onClose;
  });
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (!window.matchMedia("(max-width: 720px)").matches) return;

    let startY = 0;
    let startT = 0;
    let dy = 0;
    let dragging = false;

    const reset = () => {
      el.style.transition = "";
      el.style.transform = "";
      dragging = false;
      dy = 0;
    };

    const onStart = (e: TouchEvent) => {
      if (e.touches.length !== 1) return;
      const top = el.getBoundingClientRect().top;
      const y = e.touches[0].clientY;
      if (y - top > GRAB_ZONE) return;
      // đừng giành cử chỉ của nút / ô nhập trong vùng tiêu đề
      if ((e.target as HTMLElement).closest("input, textarea, select")) return;
      // nội dung đang cuộn xuống dở: vuốt là để cuộn lên, không phải để đóng
      const body = el.querySelector(".dialog-body");
      if (body && body.scrollTop > 0) return;
      startY = y;
      startT = performance.now();
      dy = 0;
      dragging = true;
      el.style.transition = "none";
    };
    const onMove = (e: TouchEvent) => {
      if (!dragging) return;
      dy = Math.max(0, e.touches[0].clientY - startY);
      if (dy > 0) e.preventDefault();
      el.style.transform = `translateY(${dy}px)`;
    };
    const onEnd = () => {
      if (!dragging) return;
      const v = dy / Math.max(1, performance.now() - startT);
      dragging = false;
      if (dy > CLOSE_DISTANCE || (dy > 24 && v > CLOSE_VELOCITY)) {
        el.style.transition = "transform 0.18s ease-in";
        el.style.transform = "translateY(100%)";
        window.setTimeout(() => close.current(), 170);
      } else {
        el.style.transition = "transform 0.2s cubic-bezier(0.2, 0.8, 0.2, 1)";
        el.style.transform = "";
        window.setTimeout(reset, 210);
      }
    };

    el.addEventListener("touchstart", onStart, { passive: true });
    el.addEventListener("touchmove", onMove, { passive: false });
    el.addEventListener("touchend", onEnd);
    el.addEventListener("touchcancel", onEnd);
    return () => {
      el.removeEventListener("touchstart", onStart);
      el.removeEventListener("touchmove", onMove);
      el.removeEventListener("touchend", onEnd);
      el.removeEventListener("touchcancel", onEnd);
    };
  }, [ref]);
}

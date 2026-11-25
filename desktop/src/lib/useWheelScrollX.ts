import { useEffect } from "react";

/**
 * Hàng cuộn ngang (chip, tab…): lăn bánh xe chuột (dọc) thì cuộn ngang hàng đó.
 * Chuột thường không có cuộn ngang và thanh cuộn của các hàng này bị ẩn, nên nếu không có
 * hook này thì trên máy tính không cuộn được. Đã tới đầu/cuối hàng thì để trang cuộn như thường.
 *
 * Nhận thẳng phần tử (giữ bằng callback ref: `const [row, setRow] = useState<HTMLElement | null>(null)`,
 * `ref={setRow}`) để gắn đúng cả khi hàng chỉ hiện ra sau lần vẽ đầu.
 */
export function useWheelScrollX(el: HTMLElement | null) {
  useEffect(() => {
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      // touchpad cuộn ngang sẵn, hoặc giữ Shift: trình duyệt tự lo
      if (e.shiftKey || Math.abs(e.deltaX) >= Math.abs(e.deltaY)) return;
      const max = el.scrollWidth - el.clientWidth;
      if (max <= 0) return;
      const step = e.deltaMode === 1 ? e.deltaY * 16 : e.deltaMode === 2 ? e.deltaY * el.clientWidth : e.deltaY;
      const next = Math.min(max, Math.max(0, el.scrollLeft + step));
      if (next === el.scrollLeft) return;
      e.preventDefault();
      el.scrollLeft = next;
    };
    // không passive: phải chặn được cuộn dọc của trang khi đang cuộn ngang hàng này
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, [el]);
}

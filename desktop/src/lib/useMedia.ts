import { useEffect, useState } from "react";

/** Màn hình hẹp (điện thoại): dưới mức này giao diện chuyển sang 1 cột, danh sách và khung chat tách riêng.
 *  App desktop có cửa sổ tối thiểu 760px nên không bao giờ vào chế độ này. */
export const COMPACT_QUERY = "(max-width: 720px)";

/** Thiết bị nhập bằng ngón tay (không có chuột chính xác). */
export const TOUCH_QUERY = "(pointer: coarse)";

export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() => window.matchMedia(query).matches);
  useEffect(() => {
    const mq = window.matchMedia(query);
    const onChange = () => setMatches(mq.matches);
    onChange();
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, [query]);
  return matches;
}

export const isTouchDevice = () => window.matchMedia(TOUCH_QUERY).matches;

import { useEffect } from "react";
import { displayName } from "../lib/format";
import { throwItem } from "../lib/fun";
import { useChat } from "../state/ChatProvider";

/** Bị ném đồ: món đồ bay vào giữa màn hình, bẹp dí rồi mờ dần. Không chặn thao tác. */
export function ThrowOverlay() {
  const { state, dispatch } = useChat();
  const hit = state.hit;
  useEffect(() => {
    if (!hit) return;
    const t = window.setTimeout(() => dispatch({ type: "hit.clear" }), 2800);
    return () => window.clearTimeout(t);
  }, [hit, dispatch]);
  if (!hit) return null;
  const item = throwItem(hit.item);
  const from = displayName(state.users[hit.fromId]);
  return (
    <div className={`throw-overlay throw-${hit.item}`} key={hit.key} role="status" aria-live="polite">
      <span className="throw-item" aria-hidden>
        {item?.icon ?? "🎯"}
      </span>
      <p className="throw-text">
        {from} vừa ném {item?.name ?? "đồ"} vào bạn!
      </p>
    </div>
  );
}

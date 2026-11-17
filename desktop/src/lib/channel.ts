import type { Channel, PublicUser } from "./api";
import { displayName } from "./format";

export const isDM = (c: Channel | undefined) => c?.kind === "dm";

/** Người còn lại trong cuộc trò chuyện riêng (chính mình nếu tự nhắn cho mình). */
export function dmPeerId(c: Channel, meId: number): number | null {
  if (!isDM(c)) return null;
  const others = (c.dm_members ?? []).filter((u) => u.id !== meId);
  return others[0]?.id ?? meId;
}

/** Tên hiển thị: tên người kia với tin nhắn riêng, tên kênh với kênh thường. */
export function channelTitle(c: Channel, users: Record<number, PublicUser>, meId: number): string {
  const peer = dmPeerId(c, meId);
  if (peer === null) return c.name;
  const u = users[peer] ?? c.dm_members?.find((x) => x.id === peer);
  return peer === meId ? `${displayName(u)} (bạn)` : displayName(u);
}

/** Như channelTitle nhưng kênh công khai có "#" phía trước. */
export function channelLabel(c: Channel, users: Record<number, PublicUser>, meId: number): string {
  const t = channelTitle(c, users, meId);
  return isDM(c) || c.is_private ? t : "#" + t;
}

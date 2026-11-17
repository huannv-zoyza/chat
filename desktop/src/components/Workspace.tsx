import { useCallback, useEffect, useRef, useState } from "react";
import { useChat } from "../state/ChatProvider";
import { Sidebar } from "./Sidebar";
import { ChannelView } from "./ChannelView";
import { MembersPanel } from "./MembersPanel";
import { ThreadPanel } from "./ThreadPanel";
import {
  BrowseChannelsDialog,
  ChannelSettingsDialog,
  CreateChannelDialog,
  InviteDialog,
  JoinByLinkDialog,
} from "./ChannelDialogs";
import { clearPendingInvite, peekPendingInvite } from "../lib/platform";
import { UserSettingsDialog } from "./UserSettingsDialog";
import { NewDMDialog } from "./NewDMDialog";
import { SearchDialog } from "./MessageExtras";
import { RemindersDialog } from "./Reminders";
import { isDM } from "../lib/channel";
import { load, save } from "../lib/storage";
import { COMPACT_QUERY, useMediaQuery } from "../lib/useMedia";

type DialogName =
  "create" | "browse" | "channel" | "user" | "invite" | "joinLink" | "dm" | "search" | "reminders" | null;

export function Workspace() {
  const { state, openThread } = useChat();
  // Mở bản web bằng link mời (…/invite/<mã>): hiện luôn hộp thoại tham gia.
  const [inviteCode] = useState(peekPendingInvite);
  const [dialog, setDialog] = useState<DialogName>(inviteCode ? "joinLink" : null);
  useEffect(clearPendingInvite, []);
  const [membersOpen, setMembersOpen] = useState(() => load("membersOpen", true));
  const channel = state.activeId !== null ? state.channels[state.activeId] : undefined;

  // Ctrl/Cmd + K: mở tìm kiếm
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setDialog("search");
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // Điện thoại: danh sách kênh và khung chat là 2 màn riêng (như Messenger).
  // Mở kênh thì đẩy 1 mục vào history, để nút Back của điện thoại/trình duyệt quay về danh sách.
  const compact = useMediaQuery(COMPACT_QUERY);
  const [roomOpen, setRoomOpen] = useState(false);
  const [mobileMembers, setMobileMembers] = useState(false);

  const compactRef = useRef(compact);
  compactRef.current = compact;
  const openRoom = useCallback(() => {
    if (!compactRef.current) return;
    setRoomOpen(true);
    if (!window.history.state?.goChatRoom) window.history.pushState({ goChatRoom: true }, "");
  }, []);

  // Kênh được chọn từ nơi khác (tạo kênh, vào bằng link, bấm thông báo): cũng mở màn chat.
  // Bỏ qua lần app tự chọn kênh khi mới mở, để điện thoại mở ra là thấy danh sách kênh.
  useEffect(() => {
    if (state.activeId !== null && !state.activeAuto) openRoom();
  }, [state.activeId, state.activeAuto, openRoom]);

  const closeRoom = () => {
    if (window.history.state?.goChatRoom)
      window.history.back(); // popstate sẽ đóng màn chat
    else setRoomOpen(false);
  };

  useEffect(() => {
    const onPop = () => {
      // Quay lại từ luồng: chỉ đóng luồng, vẫn ở trong kênh.
      if (window.history.state?.goChatRoom) {
        openThread(null);
        return;
      }
      setRoomOpen(false);
      setMobileMembers(false);
      openThread(null);
    };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, [openThread]);

  /* ---------- Luồng trả lời ---------- */
  const threadId = state.openThreadId;
  const thread = threadId !== null ? state.threads[threadId] : undefined;
  // Luồng chỉ hiện khi thuộc kênh đang mở (chuyển kênh thì đóng).
  const showThread = !!channel && threadId !== null && (!thread || thread.parent.channel_id === channel.id);
  useEffect(() => {
    if (threadId !== null && thread && channel && thread.parent.channel_id !== channel.id) openThread(null);
  }, [threadId, thread, channel, openThread]);

  // Điện thoại: mở luồng thì đẩy thêm 1 mục history để nút Back đóng luồng.
  useEffect(() => {
    if (compact && threadId !== null && !window.history.state?.goChatThread) {
      window.history.pushState({ goChatRoom: true, goChatThread: true }, "");
    }
  }, [compact, threadId]);
  const closeThread = () => {
    if (window.history.state?.goChatThread) window.history.back();
    else openThread(null);
  };

  // Kênh đang mở bị xoá/rời: quay về danh sách.
  useEffect(() => {
    if (compact && roomOpen && !channel) setRoomOpen(false);
  }, [compact, roomOpen, channel]);

  // Trên điện thoại panel thành viên là màn phủ, mặc định đóng (không dùng lựa chọn của bản desktop).
  // Tin nhắn riêng không có panel thành viên.
  const showMembers = !isDM(channel) && (compact ? mobileMembers : membersOpen);
  const toggleMembers = () => {
    if (compact) return setMobileMembers((v) => !v);
    setMembersOpen((v) => {
      save("membersOpen", !v);
      return !v;
    });
  };

  const cls = [
    "app",
    showThread ? "with-thread" : channel && showMembers && "with-members",
    compact && (roomOpen ? "show-room" : "show-list"),
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <div className={cls}>
      <Sidebar
        onOpenChannel={openRoom}
        onCreateChannel={() => setDialog("create")}
        onBrowseChannels={() => setDialog("browse")}
        onJoinByLink={() => setDialog("joinLink")}
        onOpenSettings={() => setDialog("user")}
        onNewDM={() => setDialog("dm")}
        onSearch={() => setDialog("search")}
        onReminders={() => setDialog("reminders")}
      />

      {channel ? (
        <ChannelView
          channel={channel}
          membersOpen={showMembers}
          onToggleMembers={toggleMembers}
          onBack={compact ? closeRoom : undefined}
          onOpenSettings={() => setDialog("channel")}
          onInvite={() => setDialog("invite")}
        />
      ) : (
        <section className="room room-empty">
          <div className="welcome">
            <span className="welcome-mark" aria-hidden>
              #
            </span>
            <h2>Chọn một kênh để bắt đầu</h2>
            <p>Tham gia kênh có sẵn của nhóm, dán link mời vào kênh riêng tư, hoặc tạo kênh mới cho dự án của bạn.</p>
            <div className="welcome-actions">
              <button className="btn btn-primary" onClick={() => setDialog("browse")}>
                Khám phá kênh
              </button>
              <button className="btn" onClick={() => setDialog("joinLink")}>
                Tham gia bằng link
              </button>
              <button className="btn" onClick={() => setDialog("create")}>
                Tạo kênh
              </button>
            </div>
          </div>
        </section>
      )}

      {showThread && channel && threadId !== null ? (
        <ThreadPanel channel={channel} parentId={threadId} onClose={closeThread} />
      ) : (
        channel && showMembers && <MembersPanel channel={channel} onClose={toggleMembers} />
      )}

      {dialog === "create" && <CreateChannelDialog onClose={() => setDialog(null)} />}
      {dialog === "browse" && (
        <BrowseChannelsDialog
          onClose={() => setDialog(null)}
          onCreate={() => setDialog("create")}
          onJoinByLink={() => setDialog("joinLink")}
        />
      )}
      {dialog === "channel" && channel && <ChannelSettingsDialog channel={channel} onClose={() => setDialog(null)} />}
      {dialog === "user" && <UserSettingsDialog onClose={() => setDialog(null)} />}
      {dialog === "invite" && channel && <InviteDialog channel={channel} onClose={() => setDialog(null)} />}
      {dialog === "reminders" && <RemindersDialog onClose={() => setDialog(null)} onJumped={openRoom} />}
      {dialog === "search" && <SearchDialog onClose={() => setDialog(null)} onJumped={openRoom} />}
      {dialog === "dm" && <NewDMDialog onClose={() => setDialog(null)} onOpened={openRoom} />}
      {dialog === "joinLink" && (
        <JoinByLinkDialog initialCode={inviteCode ?? undefined} onClose={() => setDialog(null)} />
      )}
    </div>
  );
}

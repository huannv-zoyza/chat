import { createContext, useCallback, useContext, useEffect, useMemo, useReducer, useRef, type ReactNode } from "react";
import { ApiError, type Api, type Message, type User, socketUrl } from "../lib/api";
import { ChatSocket } from "../lib/socket";
import { ServerUrlContext } from "../components/ui";
import { displayName, newClientId } from "../lib/format";
import { showNotification } from "../lib/notify";
import { channelLabel, isDM } from "../lib/channel";
import { load, save } from "../lib/storage";
import { type Action, type ChatState, channelMessages, initialState, reducer } from "./chat";

const TYPING_TTL_MS = 6_000;
const TYPING_THROTTLE_MS = 3_000;

type ChatContextValue = {
  state: ChatState;
  dispatch: React.Dispatch<Action>;
  api: Api;
  serverUrl: string;
  selectChannel: (id: number | null) => void;
  /** parentId: trả lời trong luồng của tin đó. */
  sendMessage: (channelId: number, content: string, files?: File[], parentId?: number) => void;
  /** Thả/bỏ cảm xúc; cập nhật ngay trên máy mình rồi đồng bộ với server. */
  toggleReaction: (messageId: number, emoji: string) => void;
  /** Mở luồng của tin gốc (null để đóng). */
  openThread: (parentId: number | null) => void;
  /** Sửa nội dung tin của mình. Lỗi thì ném ApiError. */
  editMessage: (messageId: number, content: string) => Promise<void>;
  /** Xoá tin (của mình, hoặc bất kỳ tin nào nếu là chủ kênh). */
  deleteMessage: (messageId: number) => Promise<void>;
  /** Mở (tạo nếu chưa có) cuộc trò chuyện riêng với userId và chuyển tới đó. */
  openDM: (userId: number) => Promise<void>;
  retryMessage: (clientId: string) => void;
  discardMessage: (clientId: string) => void;
  notifyTyping: (channelId: number) => void;
  loadOlder: (channelId: number) => Promise<void>;
  loadMembers: (channelId: number) => Promise<void>;
  reconnectNow: () => void;
  onLogout: () => void;
};

const ChatContext = createContext<ChatContextValue | null>(null);

export function useChat() {
  const ctx = useContext(ChatContext);
  if (!ctx) throw new Error("useChat phải nằm trong <ChatProvider>");
  return ctx;
}

type Props = {
  api: Api;
  serverUrl: string;
  token: string;
  me: User;
  onLogout: () => void;
  onUnauthorized: () => void;
  children: ReactNode;
};

export function ChatProvider({ api, serverUrl, token, me, onLogout, onUnauthorized, children }: Props) {
  const [state, dispatch] = useReducer(reducer, me, initialState);

  // Ref để các callback của socket luôn đọc state mới nhất mà không phải đăng ký lại.
  const stateRef = useRef(state);
  stateRef.current = state;

  const socketRef = useRef<ChatSocket | null>(null);
  const lastTypingSent = useRef<Record<number, number>>({});

  const isViewing = useCallback((channelId: number) => {
    return stateRef.current.activeId === channelId && document.hasFocus() && !document.hidden;
  }, []);

  const markActiveRead = useCallback(() => {
    const s = stateRef.current;
    if (s.activeId === null) return;
    const items = channelMessages(s, s.activeId).items;
    const last = items[items.length - 1];
    if (last) socketRef.current?.markRead(s.activeId, last.id);
    dispatch({ type: "channel.read", channelId: s.activeId });
  }, []);

  const loadLatest = useCallback(
    async (channelId: number) => {
      try {
        const res = await api.messages(channelId);
        dispatch({ type: "messages.loaded", channelId, messages: res.messages, hasMore: res.has_more, older: false });
        if (res.reads) dispatch({ type: "reads.set", channelId, reads: res.reads });
        if (isViewing(channelId)) markActiveRead();
      } catch (err) {
        console.error("tải tin nhắn:", err);
      }
    },
    [api, isViewing, markActiveRead],
  );

  const refreshChannels = useCallback(async () => {
    try {
      const channels = await api.channels();
      dispatch({ type: "channels.loaded", channels });
      const s = stateRef.current;
      if (s.activeId === null && channels.length > 0) {
        const saved = load<number | null>("activeChannel", null);
        const pick = channels.find((c) => c.id === saved) ?? channels[0];
        dispatch({ type: "channel.select", channelId: pick.id, auto: true });
      }
    } catch (err) {
      console.error("tải danh sách kênh:", err);
    }
  }, [api]);

  // Bị nhắc tên ở kênh không đang xem (hoặc cửa sổ không được focus): hiện thông báo hệ thống.
  // Bị nhắc tên, hoặc có người trả lời tin của mình, ở nơi mình không đang xem:
  // hiện thông báo hệ thống (mỗi tin tối đa 1 thông báo).
  const notifyIfMentioned = useCallback((m: Message, viewing: boolean) => {
    const s = stateRef.current;
    if (m.user_id === s.me.id) return;
    const channel = s.channels[m.channel_id];
    const direct = isDM(channel);
    const mentioned = !!m.mentions?.includes(s.me.id);
    const repliedToMe = !!m.parent_id && m.parent_user_id === s.me.id;
    const seeingIt = viewing && (!m.parent_id || s.openThreadId === m.parent_id);
    if (seeingIt || (!direct && !mentioned && !repliedToMe)) return;
    const where = channel ? channelLabel(channel, s.users, s.me.id) : "kênh";
    const who = displayName(s.users[m.user_id] ?? m.user);
    const body = m.content || (m.attachments?.length ? `Đã gửi ${m.attachments.length} file` : "");
    const title = direct
      ? `${who} nhắn cho bạn`
      : mentioned
        ? `${who} nhắc bạn trong ${where}`
        : `${who} trả lời bạn trong ${where}`;
    void showNotification(title, body.slice(0, 200), () => {
      dispatch({ type: "channel.select", channelId: m.channel_id });
      if (m.parent_id) dispatch({ type: "thread.open", parentId: m.parent_id });
    });
  }, []);

  const loadThread = useCallback(
    async (parentId: number) => {
      try {
        const res = await api.thread(parentId);
        dispatch({ type: "thread.loaded", parent: res.parent, replies: res.replies });
      } catch (err) {
        console.error("tải luồng:", err);
      }
    },
    [api],
  );

  // Gửi tin kèm file qua HTTP. Thành công thì tự thêm tin vào danh sách (phòng khi socket
  // đang rớt chưa kịp nhận "message.new"); reducer bỏ trùng theo id.
  const upload = useCallback(
    async (clientId: string, channelId: number, content: string, files: File[], parentId?: number) => {
      try {
        const msg = await api.sendWithFiles(channelId, { content, clientId, files, parentId }, (progress) =>
          dispatch({ type: "pending.progress", clientId, progress }),
        );
        dispatch({ type: "message.new", message: msg, clientId, countUnread: false });
      } catch (err) {
        dispatch({
          type: "pending.failed",
          clientId,
          error: err instanceof ApiError ? err.message : "Không tải được file lên.",
        });
      }
    },
    [api],
  );

  /* ---------- Kết nối socket ---------- */

  useEffect(() => {
    const sock = new ChatSocket(socketUrl(serverUrl, token));
    socketRef.current = sock;
    let everReady = false;

    const offs = [
      sock.onStatus((status) => {
        dispatch({ type: "status", status });
        if (status === "unauthorized") onUnauthorized();
      }),

      sock.on("ready", (ev) => {
        dispatch({ type: "ready", me: ev.user, online: ev.online_user_ids });
        if (everReady) {
          // Kết nối lại: có thể đã lỡ tin trong lúc mất mạng.
          dispatch({ type: "messages.stale" });
          void refreshChannels();
          const active = stateRef.current.activeId;
          if (active !== null) void loadLatest(active);
          const thread = stateRef.current.openThreadId;
          if (thread !== null) void loadThread(thread);
        }
        everReady = true;
      }),

      sock.on("message.new", (ev) => {
        const viewing = isViewing(ev.message.channel_id);
        dispatch({ type: "message.new", message: ev.message, clientId: ev.client_id, countUnread: !viewing });
        if (viewing && ev.message.user_id !== stateRef.current.me.id) {
          sock.markRead(ev.message.channel_id, ev.message.id);
        }
        notifyIfMentioned(ev.message, viewing);
      }),

      sock.on("message.updated", (ev) => dispatch({ type: "message.updated", message: ev.message })),

      sock.onSendFailed((clientId, reason) => dispatch({ type: "pending.failed", clientId, error: reason })),

      sock.on("typing", (ev) =>
        dispatch({ type: "typing", channelId: ev.channel_id, userId: ev.user_id, until: Date.now() + TYPING_TTL_MS }),
      ),
      sock.on("presence", (ev) => dispatch({ type: "presence", userId: ev.user_id, online: ev.online })),
      sock.on("user.updated", (ev) => {
        dispatch({ type: "user.updated", user: ev.user });
      }),

      sock.on("channel.joined", (ev) => dispatch({ type: "channel.upsert", channel: ev.channel })),
      sock.on("channel.updated", (ev) => dispatch({ type: "channel.upsert", channel: ev.channel, keepUnread: true })),
      sock.on("channel.left", (ev) => dispatch({ type: "channel.removed", channelId: ev.channel_id })),
      sock.on("channel.deleted", (ev) => dispatch({ type: "channel.removed", channelId: ev.channel_id })),
      sock.on("member.joined", (ev) => dispatch({ type: "member.joined", channelId: ev.channel_id, user: ev.user })),
      sock.on("member.left", (ev) => dispatch({ type: "member.left", channelId: ev.channel_id, userId: ev.user_id })),
      sock.on("reaction.updated", (ev) =>
        dispatch({ type: "reactions.set", messageId: ev.message_id, reactions: ev.reactions }),
      ),
      sock.on("thread.updated", (ev) =>
        dispatch({ type: "thread.updated", messageId: ev.message_id, thread: ev.thread }),
      ),
      sock.on("read.updated", (ev) =>
        dispatch({ type: "read.updated", channelId: ev.channel_id, userId: ev.user_id, messageId: ev.message_id }),
      ),
    ];

    sock.connect();
    void refreshChannels();

    return () => {
      offs.forEach((off) => off());
      sock.close();
      socketRef.current = null;
    };
  }, [serverUrl, token, onUnauthorized, refreshChannels, loadLatest, isViewing, notifyIfMentioned, loadThread]);

  // Số tin chưa đọc trên tiêu đề tab (bản web), ví dụ "(3) Go Chat".
  useEffect(() => {
    const total = Object.values(state.channels).reduce((n, c) => n + c.unread_count, 0);
    document.title = total > 0 ? `(${total}) Go Chat` : "Go Chat";
  }, [state.channels]);

  /* ---------- Kênh đang mở ---------- */

  useEffect(() => {
    if (state.activeId === null) return;
    save("activeChannel", state.activeId);
    if (!channelMessages(stateRef.current, state.activeId).loaded) void loadLatest(state.activeId);
    else markActiveRead();
  }, [state.activeId, loadLatest, markActiveRead]);

  // Quay lại cửa sổ: đánh dấu đã đọc kênh đang mở.
  useEffect(() => {
    const onFocus = () => markActiveRead();
    window.addEventListener("focus", onFocus);
    document.addEventListener("visibilitychange", onFocus);
    return () => {
      window.removeEventListener("focus", onFocus);
      document.removeEventListener("visibilitychange", onFocus);
    };
  }, [markActiveRead]);

  // Dọn trạng thái "đang gõ" đã hết hạn.
  useEffect(() => {
    const t = window.setInterval(() => dispatch({ type: "typing.expire", now: Date.now() }), 1_000);
    return () => window.clearInterval(t);
  }, []);

  /* ---------- Hành động cho UI ---------- */

  // Các hành động KHÔNG phụ thuộc state (đọc state qua stateRef), nên giữ nguyên identity giữa
  // các lần render. Nếu không, effect nào dùng chúng làm dependency sẽ chạy lại sau mỗi tin nhắn.
  const actions = useMemo<Omit<ChatContextValue, "state">>(
    () => ({
      dispatch,
      api,
      serverUrl,
      onLogout,
      selectChannel: (id) => dispatch({ type: "channel.select", channelId: id }),

      sendMessage: (channelId, content, files, parentId) => {
        const clientId = newClientId();
        const hasFiles = !!files && files.length > 0;
        dispatch({
          type: "pending.add",
          pending: {
            clientId,
            channelId,
            content,
            createdAt: new Date().toISOString(),
            status: "sending",
            ...(hasFiles ? { files, progress: 0 } : {}),
            ...(parentId ? { parentId } : {}),
          },
        });
        if (hasFiles) void upload(clientId, channelId, content, files, parentId);
        else socketRef.current?.sendMessage(channelId, content, clientId, parentId);
        lastTypingSent.current[channelId] = 0;
      },

      retryMessage: (clientId) => {
        const p = stateRef.current.pending.find((x) => x.clientId === clientId);
        if (!p) return;
        dispatch({ type: "pending.retry", clientId });
        if (p.files?.length) void upload(clientId, p.channelId, p.content, p.files, p.parentId);
        else socketRef.current?.sendMessage(p.channelId, p.content, clientId, p.parentId);
      },

      toggleReaction: (messageId, emoji) => {
        dispatch({ type: "reaction.toggleLocal", messageId, emoji });
        api
          .toggleReaction(messageId, emoji)
          .then((res) => dispatch({ type: "reactions.set", messageId, reactions: res.reactions }))
          .catch((err) => {
            console.error("thả cảm xúc:", err);
            dispatch({ type: "reaction.toggleLocal", messageId, emoji }); // hoàn tác
          });
      },

      editMessage: async (messageId, content) => {
        const msg = await api.editMessage(messageId, content);
        dispatch({ type: "message.updated", message: msg });
      },

      deleteMessage: async (messageId) => {
        const msg = await api.deleteMessage(messageId);
        dispatch({ type: "message.updated", message: msg });
      },

      openDM: async (userId) => {
        const ch = await api.openDM(userId);
        dispatch({ type: "channel.upsert", channel: ch, keepUnread: true });
        dispatch({ type: "channel.select", channelId: ch.id });
      },

      openThread: (parentId) => {
        dispatch({ type: "thread.open", parentId });
        if (parentId !== null) void loadThread(parentId);
      },

      discardMessage: (clientId) => {
        socketRef.current?.cancelMessage(clientId);
        dispatch({ type: "pending.remove", clientId });
      },

      notifyTyping: (channelId) => {
        const now = Date.now();
        if (now - (lastTypingSent.current[channelId] ?? 0) < TYPING_THROTTLE_MS) return;
        lastTypingSent.current[channelId] = now;
        socketRef.current?.sendTyping(channelId);
      },

      loadOlder: async (channelId) => {
        const cm = channelMessages(stateRef.current, channelId);
        if (cm.loadingOlder || !cm.hasMore || cm.items.length === 0) return;
        dispatch({ type: "messages.loadingOlder", channelId, loading: true });
        try {
          const res = await api.messages(channelId, cm.items[0].id);
          dispatch({ type: "messages.loaded", channelId, messages: res.messages, hasMore: res.has_more, older: true });
        } catch (err) {
          console.error("tải tin cũ:", err);
          dispatch({ type: "messages.loadingOlder", channelId, loading: false });
        }
      },

      loadMembers: async (channelId) => {
        const members = await api.members(channelId);
        dispatch({ type: "members.loaded", channelId, members });
      },

      reconnectNow: () => socketRef.current?.reconnectNow(),
    }),
    [api, serverUrl, onLogout, upload, loadThread],
  );

  const value = useMemo<ChatContextValue>(() => ({ state, ...actions }), [state, actions]);

  return (
    <ChatContext.Provider value={value}>
      <ServerUrlContext.Provider value={serverUrl}>{children}</ServerUrlContext.Provider>
    </ChatContext.Provider>
  );
}

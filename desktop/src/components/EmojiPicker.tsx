import { useEffect, useRef, useState } from "react";
import { load, save } from "../lib/storage";

// Bộ emoji gọn, chia nhóm theo cách dùng trong chat nhóm làm việc.
const GROUPS: { id: string; label: string; icon: string; emojis: string }[] = [
  {
    id: "smileys",
    label: "Mặt cười",
    icon: "😀",
    emojis:
      "😀 😃 😄 😁 😆 😅 🤣 😂 🙂 🙃 😉 😊 😇 🥰 😍 🤩 😘 😗 😚 😋 😛 😜 🤪 😝 🤑 🤗 🤭 🤫 🤔 🤐 🤨 😐 😑 😶 😏 😒 🙄 😬 😮‍💨 🤥 😌 😔 😪 🤤 😴 😷 🤒 🤕 🤢 🤮 🥵 🥶 🥴 😵 🤯 🤠 🥳 😎 🤓 🧐 😕 😟 🙁 😮 😯 😲 😳 🥺 😦 😧 😨 😰 😥 😢 😭 😱 😖 😣 😞 😓 😩 😫 🥱 😤 😡 😠 🤬 😈 💀 💩 🤡 👻 👽 🤖",
  },
  {
    id: "gestures",
    label: "Cử chỉ",
    icon: "👍",
    emojis:
      "👍 👎 👌 🤌 🤏 ✌️ 🤞 🤟 🤘 🤙 👈 👉 👆 👇 ☝️ ✋ 🤚 🖐️ 🖖 👋 🤝 🙏 👏 🙌 👐 🤲 ✍️ 💪 🦾 🫶 👀 🧠 🫡 🤷 🤦 🙋 🙆 🙅 💁 🙇",
  },
  {
    id: "hearts",
    label: "Tim & ký hiệu",
    icon: "❤️",
    emojis:
      "❤️ 🧡 💛 💚 💙 💜 🖤 🤍 🤎 💔 ❤️‍🔥 💕 💞 💓 💗 💖 💘 💝 ✅ ☑️ ✔️ ❌ ❎ ⭕ ❗ ❓ ‼️ ⁉️ 💯 🔥 ✨ ⭐ 🌟 💫 ⚡ 💥 💢 💬 💭 🗯️ 🔔 🔕 📌 📍 🚩 🎯 ⚠️ 🚫 ⛔ 🆗 🆕 🆒 🔝 ➕ ➖ ➡️ ⬅️ ⬆️ ⬇️ 🔄",
  },
  {
    id: "work",
    label: "Công việc",
    icon: "💻",
    emojis:
      "💻 🖥️ ⌨️ 🖱️ 📱 ☎️ 📞 📧 📨 📩 📤 📥 📦 📁 📂 🗂️ 📄 📃 📑 📊 📈 📉 🗒️ 📝 ✏️ 🖊️ 📎 🖇️ 📏 📐 ✂️ 🗑️ 🔒 🔓 🔑 🔍 🔎 🧪 🛠️ 🔧 🔨 ⚙️ 🧩 🐛 🚀 🎉 🎊 🏆 🥇 📅 📆 ⏰ ⏳ ⌛ 🕐 💡 📢 📣",
  },
  {
    id: "nature",
    label: "Động vật & thiên nhiên",
    icon: "🐶",
    emojis:
      "🐶 🐱 🐭 🐹 🐰 🦊 🐻 🐼 🐨 🐯 🦁 🐮 🐷 🐸 🐵 🙈 🙉 🙊 🐔 🐧 🐦 🐤 🦆 🦉 🐺 🐴 🦄 🐝 🦋 🐌 🐞 🐢 🐍 🐙 🐠 🐬 🐳 🦈 🌸 🌼 🌻 🌹 🌷 🌱 🌿 🍀 🍁 🍂 🌵 🌴 🌲 ☀️ 🌤️ ⛅ 🌧️ ⛈️ 🌈 ❄️ ☃️ 🌙 🌊",
  },
  {
    id: "food",
    label: "Đồ ăn & uống",
    icon: "🍜",
    emojis:
      "🍏 🍎 🍐 🍊 🍋 🍌 🍉 🍇 🍓 🍒 🍑 🥭 🍍 🥥 🥑 🌽 🌶️ 🥕 🥔 🍞 🥖 🧀 🥚 🍳 🥓 🍗 🍖 🌭 🍔 🍟 🍕 🥪 🌮 🍜 🍲 🍛 🍣 🍱 🥟 🍤 🍙 🍚 🍘 🍥 🍦 🍰 🎂 🍮 🍭 🍬 🍫 🍿 🍩 🍪 ☕ 🍵 🧋 🥤 🍺 🍻 🥂 🍷",
  },
  {
    id: "activity",
    label: "Hoạt động & đi lại",
    icon: "⚽",
    emojis:
      "⚽ 🏀 🏈 ⚾ 🎾 🏐 🏓 🏸 🥅 ⛳ 🎣 🏊 🚴 🏃 🧘 🎮 🕹️ 🎲 ♟️ 🎯 🎳 🎤 🎧 🎸 🎹 🎬 🎨 📷 🎁 🎈 🏖️ 🏝️ ⛰️ 🏕️ 🏠 🏢 🏥 🏫 🚗 🚕 🚌 🏍️ 🛵 🚲 ✈️ 🚆 🚢 ⛽ 🗺️ 🧳",
  },
];

const LIST: Record<string, string[]> = Object.fromEntries(
  GROUPS.map((g) => [g.id, g.emojis.split(" ").filter(Boolean)]),
);

const RECENT_KEY = "recentEmojis";
const RECENT_MAX = 24;

export function rememberEmoji(e: string) {
  const list = load<string[]>(RECENT_KEY, []).filter((x) => x !== e);
  save(RECENT_KEY, [e, ...list].slice(0, RECENT_MAX));
}

type Props = {
  onPick: (emoji: string) => void;
  onClose: () => void;
  /** Thêm class/vị trí khi dùng làm bảng chọn nổi (thả cảm xúc vào tin nhắn). */
  className?: string;
  style?: React.CSSProperties;
};

/** Bảng chọn emoji dạng popover: nhóm theo tab, có "Dùng gần đây". Esc hoặc bấm ra ngoài để đóng. */
export function EmojiPicker({ onPick, onClose, className, style }: Props) {
  const recent = load<string[]>(RECENT_KEY, []);
  const [tab, setTab] = useState(recent.length ? "recent" : GROUPS[0].id);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    // trì hoãn 1 nhịp để cú bấm mở popover không đóng nó luôn
    const t = window.setTimeout(() => document.addEventListener("mousedown", onDown));
    document.addEventListener("keydown", onKey, true);
    return () => {
      window.clearTimeout(t);
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey, true);
    };
  }, [onClose]);

  const tabs = [
    ...(recent.length ? [{ id: "recent", label: "Dùng gần đây", icon: "🕘" }] : []),
    ...GROUPS.map(({ id, label, icon }) => ({ id, label, icon })),
  ];
  const emojis = tab === "recent" ? recent : LIST[tab];
  const current = tabs.find((t) => t.id === tab);

  return (
    <div
      className={`emoji-picker${className ? " " + className : ""}`}
      style={style}
      ref={ref}
      role="dialog"
      aria-label="Chọn biểu tượng cảm xúc"
    >
      <div className="emoji-tabs" role="tablist">
        {tabs.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={tab === t.id}
            className="emoji-tab"
            title={t.label}
            onClick={() => setTab(t.id)}
          >
            {t.icon}
          </button>
        ))}
      </div>
      <p className="emoji-group-label">{current?.label}</p>
      <div className="emoji-grid">
        {emojis.map((e) => (
          <button
            key={e}
            type="button"
            className="emoji-btn"
            onClick={() => {
              rememberEmoji(e);
              onPick(e);
            }}
          >
            {e}
          </button>
        ))}
      </div>
    </div>
  );
}

#!/usr/bin/env python3
"""Tải hình emoji cho nông trại từ Microsoft Fluent Emoji (bản 3D, giấy phép MIT) về
desktop/src/assets/emoji/<mã>.webp, để emoji trông như nhau trên mọi máy (iPhone vẽ 🪙 màu bạc
như cục đá, mỗi hệ điều hành vẽ người/vật một kiểu).

Chạy lại khi nông trại dùng thêm emoji mới:  python3 scripts/fetch-farm-emoji.py
Danh sách emoji: EMOJI dưới đây (phải khớp các emoji trong desktop/src/components/Farm*.tsx và
catalog ở store/farm*.go). Mã tệp = các codepoint hex nối bằng "-", bỏ FE0F (xem lib/emoji.ts).
"""
import io
import json
import os
import sys
import unicodedata
import urllib.request

from PIL import Image

EMOJI = (
    # tiền, nhân vật, vật nuôi, xe, cảnh
    "🪙 🧑‍🌾 🐕 🦆 🐓 🐔 🐄 🐐 🐑 🐖 🚚 🌾 🌷 🌻 🌼 🌸 🐾"
    # cây trồng & sản phẩm
    " 🥬 🥕 🍅 🌽 🍓 🍉 🎃 🥚 🥛 🧀 🧶 🍄 🌱 🌿 🥀 🐛 💧 ⭐ 🍽️"
    # biển, nhãn, giao diện
    " 🏚️ 📦 🪧 🔨 📋 🛒 🏡 🏠 📜 🏆 🔒 🧺 🫣 🎉 👀 😅 ⏱️ 📭 🎁 🔥 🟫 🛖 🗑️ 💨 🥇 🥈 🥉"
    # xưởng chế biến: món và biểu tượng
    " 🥗 🍿 🍙 🍮 🥤 🍕 🍧 🧥 🥧 🍲 🍳 🏭 ♨️"
    # kết đôi, chọn chỗ xây
    " 💞 💌 💔 💗 💍 🚧 👆 🌱"
).split()

# Tên thư mục trong repo khác tên Unicode
NAMES = {
    "🧑‍🌾": "Farmer",
    "🐑": "Ewe",
    "🎃": "Jack-o-lantern",
    "🍽️": "Fork and knife with plate",
    "📭": "Open mailbox with lowered flag",
    "🥇": "1st place medal",
    "🥈": "2nd place medal",
    "🥉": "3rd place medal",
    "🌾": "Sheaf of rice",
    "🌽": "Ear of corn",
    "🧶": "Yarn",
    "⭐": "Star",
    "🏚️": "Derelict house",
    "🛒": "Shopping cart",
    "🏠": "House",
    "🔒": "Locked",
    "😅": "Grinning face with sweat",
    "🎁": "Wrapped gift",
    "🟫": "Brown square",
    "💨": "Dashing away",
    "🍕": "Pizza",
    "♨️": "Hot springs",
    "💞": "Revolving hearts",
    "💌": "Love letter",
    "💔": "Broken heart",
    "💗": "Growing heart",
    "💍": "Ring",
    "🚧": "Construction",
    "👆": "Backhand index pointing up",
}

REPO = "https://cdn.jsdelivr.net/gh/microsoft/fluentui-emoji@main/assets/"
TREE = "https://api.github.com/repos/microsoft/fluentui-emoji/git/trees/main?recursive=1"
SIZE = 128
OUT = os.path.join(os.path.dirname(__file__), "..", "desktop", "src", "assets", "emoji")


def code(e: str) -> str:
    return "-".join(f"{ord(c):x}" for c in e if c != "️")


def get(url: str) -> bytes:
    req = urllib.request.Request(url, headers={"User-Agent": "farm-emoji-fetch"})
    with urllib.request.urlopen(req, timeout=60) as r:
        return r.read()


def main() -> int:
    paths = [t["path"] for t in json.loads(get(TREE))["tree"]]
    pngs = {}
    for p in paths:
        parts = p.split("/")
        if len(parts) >= 4 and parts[0] == "assets" and "/3D/" in p and p.endswith(".png"):
            folder = parts[1]
            # có biến thể màu da: lấy Default
            if len(parts) == 5 and parts[2] != "Default":
                continue
            pngs.setdefault(folder.lower(), p)
    os.makedirs(OUT, exist_ok=True)
    missing = []
    for e in EMOJI:
        name = NAMES.get(e) or unicodedata.name(e.replace("️", "")[0]).capitalize()
        p = pngs.get(name.lower())
        if not p:
            missing.append(f"{e} ({name})")
            continue
        url = REPO + urllib.request.quote(p[len("assets/"):])
        try:
            img = Image.open(io.BytesIO(get(url)))
        except Exception as err:  # noqa: BLE001 — một hình lỗi thì báo, vẫn tải các hình khác
            missing.append(f"{e} ({url}: {err})")
            continue
        img = img.convert("RGBA").resize((SIZE, SIZE), Image.LANCZOS)
        dst = os.path.join(OUT, code(e) + ".webp")
        img.save(dst, "WEBP", quality=88, method=6)
        print(f"{e}  {p}  -> {os.path.basename(dst)} ({os.path.getsize(dst)} B)")
    with open(os.path.join(OUT, "LICENSE.txt"), "w") as f:
        f.write(
            "Emoji images from Microsoft Fluent Emoji (https://github.com/microsoft/fluentui-emoji),\n"
            "MIT License, Copyright (c) Microsoft Corporation. Resized to 128px WebP.\n"
        )
    if missing:
        print("KHÔNG TÌM THẤY:", ", ".join(missing), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

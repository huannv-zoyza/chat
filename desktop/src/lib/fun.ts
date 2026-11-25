/** Đồ ném được (khớp throwItems ở controller/fun.go). */
export const THROW_ITEMS = [
  { id: "dep", icon: "🩴", name: "dép" },
  { id: "cachua", icon: "🍅", name: "cà chua" },
  { id: "trung", icon: "🥚", name: "trứng" },
  { id: "banhkem", icon: "🎂", name: "bánh kem" },
  { id: "hoa", icon: "🌹", name: "hoa hồng" },
];

export const throwItem = (id: string) => THROW_ITEMS.find((t) => t.id === id);

export const NICKNAME_MAX = 32;

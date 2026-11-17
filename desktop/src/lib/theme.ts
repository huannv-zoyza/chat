import { load, save } from "./storage";

export type ThemePref = "system" | "light" | "dark";

export function getTheme(): ThemePref {
  return load<ThemePref>("theme", "system");
}

/** Gắn data-theme lên <html>; "system" thì bỏ thuộc tính để CSS theo prefers-color-scheme. */
export function applyTheme(t: ThemePref) {
  const root = document.documentElement;
  if (t === "system") root.removeAttribute("data-theme");
  else root.setAttribute("data-theme", t);
  save("theme", t);
}

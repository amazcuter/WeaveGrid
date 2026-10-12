import { useEffect, useState } from "react";

export type Theme = "light" | "dark";
const themeKey = "novelflow-theme";
export function initialTheme(): Theme {
  try {
    const stored = localStorage.getItem(themeKey);
    if (stored === "light" || stored === "dark") return stored;
  } catch {
    /* 禁用存储时仍可切换本次会话主题。 */
  }
  return window.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";
}
export function applyTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme;
  document.documentElement.style.colorScheme = theme;
}
export function useTheme() {
  const [theme, setTheme] = useState<Theme>(initialTheme);
  useEffect(() => {
    applyTheme(theme);
    try {
      localStorage.setItem(themeKey, theme);
    } catch {
      /* 可继续使用。 */
    }
  }, [theme]);
  return [theme, setTheme] as const;
}

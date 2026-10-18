import { useSyncExternalStore } from "react"

export type ThemeChoice = "light" | "dark" | "system"
export type ResolvedTheme = "light" | "dark"

/** The key public/theme-init.js reads before the first paint. */
export const THEME_KEY = "netscribe.theme"

/** An unknown or missing stored value means the system setting. */
export function parseChoice(raw: string | null | undefined): ThemeChoice {
  return raw === "light" || raw === "dark" ? raw : "system"
}

export function resolveTheme(choice: ThemeChoice, systemDark: boolean): ResolvedTheme {
  if (choice === "system") return systemDark ? "dark" : "light"
  return choice
}

export function applyTheme(theme: ResolvedTheme, root: HTMLElement = document.documentElement) {
  root.classList.toggle("dark", theme === "dark")
}

function systemQuery(): MediaQueryList | null {
  return typeof window.matchMedia === "function"
    ? window.matchMedia("(prefers-color-scheme: dark)")
    : null
}

function readStored(): ThemeChoice {
  try {
    return parseChoice(localStorage.getItem(THEME_KEY))
  } catch {
    return "system"
  }
}

let choice: ThemeChoice = "system"
let systemDark = false
const listeners = new Set<() => void>()

function update() {
  applyTheme(resolveTheme(choice, systemDark))
  listeners.forEach((listener) => listener())
}

/**
 * Reads the stored choice, applies it and follows the system setting from now on.
 * Call it once at startup. It returns a function that stops following the system.
 */
export function initTheme(): () => void {
  const query = systemQuery()
  choice = readStored()
  systemDark = query?.matches ?? false
  const onChange = (event: MediaQueryListEvent) => {
    systemDark = event.matches
    update()
  }
  query?.addEventListener("change", onChange)
  update()
  return () => query?.removeEventListener("change", onChange)
}

export function setTheme(next: ThemeChoice) {
  choice = next
  try {
    localStorage.setItem(THEME_KEY, next)
  } catch {
    // The choice still applies until the page closes.
  }
  update()
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

/** The stored choice and the theme it resolves to right now. */
export function useTheme(): { choice: ThemeChoice; resolved: ResolvedTheme } {
  const current = useSyncExternalStore(subscribe, () => choice)
  const dark = useSyncExternalStore(subscribe, () => systemDark)
  return { choice: current, resolved: resolveTheme(current, dark) }
}

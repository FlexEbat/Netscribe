import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { applyTheme, initTheme, parseChoice, resolveTheme, setTheme, THEME_KEY } from "@/lib/theme"

describe("parseChoice", () => {
  it("keeps the two explicit choices", () => {
    expect(parseChoice("light")).toBe("light")
    expect(parseChoice("dark")).toBe("dark")
  })

  it("falls back to the system setting for anything else", () => {
    for (const raw of [null, undefined, "", "system", "DARK", "blue"]) {
      expect(parseChoice(raw)).toBe("system")
    }
  })
})

describe("resolveTheme", () => {
  it("lets an explicit choice win over the system setting", () => {
    expect(resolveTheme("light", true)).toBe("light")
    expect(resolveTheme("dark", false)).toBe("dark")
  })

  it("follows the system setting in system mode", () => {
    expect(resolveTheme("system", true)).toBe("dark")
    expect(resolveTheme("system", false)).toBe("light")
  })
})

describe("applyTheme", () => {
  it("sets and removes the dark class", () => {
    const root = document.createElement("html")
    applyTheme("dark", root)
    expect(root.classList.contains("dark")).toBe(true)
    applyTheme("light", root)
    expect(root.classList.contains("dark")).toBe(false)
  })
})

/** A matchMedia stand-in whose answer the test changes. */
function stubSystem(dark: boolean) {
  const handlers = new Set<(event: MediaQueryListEvent) => void>()
  const query = {
    matches: dark,
    addEventListener: (_: string, h: (event: MediaQueryListEvent) => void) => handlers.add(h),
    removeEventListener: (_: string, h: (event: MediaQueryListEvent) => void) => handlers.delete(h),
  }
  vi.stubGlobal("matchMedia", () => query)
  return {
    change(next: boolean) {
      query.matches = next
      handlers.forEach((h) => h({ matches: next } as MediaQueryListEvent))
    },
    listeners: () => handlers.size,
  }
}

describe("the theme store", () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.classList.remove("dark")
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  const isDark = () => document.documentElement.classList.contains("dark")

  it("applies the stored choice at startup", () => {
    stubSystem(false)
    localStorage.setItem(THEME_KEY, "dark")
    const stop = initTheme()
    expect(isDark()).toBe(true)
    stop()
  })

  it("follows the system setting in system mode and reacts when it changes", () => {
    const system = stubSystem(false)
    const stop = initTheme()
    expect(isDark()).toBe(false)
    system.change(true)
    expect(isDark()).toBe(true)
    system.change(false)
    expect(isDark()).toBe(false)
    stop()
    expect(system.listeners()).toBe(0)
  })

  it("ignores a system change while a theme is chosen", () => {
    const system = stubSystem(false)
    const stop = initTheme()
    setTheme("light")
    system.change(true)
    expect(isDark()).toBe(false)
    stop()
  })

  it("stores the choice and applies it at once", () => {
    stubSystem(false)
    const stop = initTheme()
    setTheme("dark")
    expect(localStorage.getItem(THEME_KEY)).toBe("dark")
    expect(isDark()).toBe(true)
    setTheme("system")
    expect(localStorage.getItem(THEME_KEY)).toBe("system")
    expect(isDark()).toBe(false)
    stop()
  })

  it("keeps working when storage is blocked", () => {
    stubSystem(true)
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked")
    })
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked")
    })
    const stop = initTheme()
    expect(isDark()).toBe(true)
    expect(() => setTheme("light")).not.toThrow()
    expect(isDark()).toBe(false)
    stop()
  })
})

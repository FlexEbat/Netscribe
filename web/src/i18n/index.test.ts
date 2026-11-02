import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  apiErrorText,
  detectLang,
  formatDateTime,
  langStorageKey,
  resetLang,
  setLang,
  t,
} from "@/i18n"

beforeEach(() => {
  localStorage.clear()
  resetLang()
})

afterEach(() => {
  vi.restoreAllMocks()
  localStorage.clear()
  resetLang()
})

describe("t", () => {
  it("fills the placeholders and keeps English by default", () => {
    expect(t("account.signedInAs", { name: "olga" })).toBe("Signed in as olga")
  })

  it("leaves a placeholder that has no value as it is", () => {
    expect(t("account.signedInAs")).toBe("Signed in as {name}")
  })

  it("picks the English plural form", () => {
    expect(t("bulkBar.selected", { n: 1 })).toBe("1 device selected")
    expect(t("bulkBar.selected", { n: 2 })).toBe("2 devices selected")
  })

  it("picks the right Russian plural form for 1, 2, 5, 11, 21 and 22 devices", () => {
    setLang("ru")
    const word = (n: number) => t("bulkBar.selected", { n })
    expect(word(1)).toBe("Выбрано 1 устройство")
    expect(word(2)).toBe("Выбрано 2 устройства")
    expect(word(5)).toBe("Выбрано 5 устройств")
    expect(word(11)).toBe("Выбрано 11 устройств")
    expect(word(21)).toBe("Выбрано 21 устройство")
    expect(word(22)).toBe("Выбрано 22 устройства")
  })
})

describe("the language", () => {
  it("survives a reload: the stored value wins", () => {
    setLang("ru")
    expect(localStorage.getItem(langStorageKey)).toBe("ru")
    expect(detectLang()).toBe("ru")
    expect(t("nav.settings")).toBe("Настройки")
  })

  it("follows the browser language when nothing is stored", () => {
    vi.spyOn(navigator, "language", "get").mockReturnValue("ru-RU")
    expect(detectLang()).toBe("ru")
    vi.spyOn(navigator, "language", "get").mockReturnValue("de-DE")
    expect(detectLang()).toBe("en")
  })

  it("treats any other stored value as unset", () => {
    vi.spyOn(navigator, "language", "get").mockReturnValue("ru")
    localStorage.setItem(langStorageKey, "fr")
    expect(detectLang()).toBe("ru")
  })

  it("sets the lang of the page", () => {
    setLang("ru")
    expect(document.documentElement.lang).toBe("ru")
    setLang("en")
    expect(document.documentElement.lang).toBe("en")
  })

  it("writes dates in the language of the panel", () => {
    const when = "2026-10-09T12:00:00.000Z"
    setLang("en")
    const english = formatDateTime(when)
    setLang("ru")
    expect(formatDateTime(when)).not.toBe(english)
    expect(formatDateTime("not a date")).toBe("–")
  })
})

describe("apiErrorText", () => {
  it("shows a known server error in Russian and an unknown one as it came", () => {
    setLang("ru")
    expect(apiErrorText("the password is not right")).toBe("пароль неверный")
    expect(apiErrorText("a brand new failure nobody translated")).toBe(
      "a brand new failure nobody translated",
    )
    expect(apiErrorText("Request failed (502)")).toBe("Запрос не выполнен (502)")
  })

  it("keeps the server text in English", () => {
    expect(apiErrorText("the password is not right")).toBe("the password is not right")
  })
})

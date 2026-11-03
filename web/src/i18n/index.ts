import { useSyncExternalStore } from "react"
import { en } from "@/i18n/en"
import { ru } from "@/i18n/ru"

/*
 * A small translation module without a library. English is the language of the code: en.ts
 * holds every key, and the type Key comes from it. A value is a string with {name}
 * placeholders, or an object with one string per plural form of Intl.PluralRules.
 */

export type Lang = "en" | "ru"
export type Key = keyof typeof en
export type Plural = { other: string } & Partial<
  Record<"zero" | "one" | "two" | "few" | "many", string>
>
export type Entry = string | Plural
export type Params = Record<string, string | number>

export const langStorageKey = "netscribe.lang"
export const languages: readonly Lang[] = ["en", "ru"]
export const languageNames: Record<Lang, string> = { en: "English", ru: "Русский" }

const dictionaries: Record<Lang, Partial<Record<Key, Entry>>> = { en, ru }

/** Reads the stored language. Anything else means the browser language, then English. */
export function detectLang(): Lang {
  let stored: string | null = null
  try {
    stored = localStorage.getItem(langStorageKey)
  } catch {
    // Storage can be blocked; the browser language decides then.
  }
  if (stored === "en" || stored === "ru") return stored
  const browser = typeof navigator === "undefined" ? "" : (navigator.language ?? "")
  return browser.toLowerCase().startsWith("ru") ? "ru" : "en"
}

let current: Lang = detectLang()
const listeners = new Set<() => void>()

function apply(lang: Lang) {
  current = lang
  if (typeof document !== "undefined") document.documentElement.lang = lang
}
apply(current)

/** Reads a language from a menu value; anything unknown means English. */
export function parseLang(value: string): Lang {
  return value === "ru" ? "ru" : "en"
}

export function getLang(): Lang {
  return current
}

/** Switches the language, remembers it and tells the components that show it. */
export function setLang(lang: Lang) {
  try {
    localStorage.setItem(langStorageKey, lang)
  } catch {
    // The language still changes for this visit when storage is blocked.
  }
  apply(lang)
  for (const fn of listeners) fn()
}

/** Forgets the stored language, so the browser language decides again. */
export function resetLang() {
  try {
    localStorage.removeItem(langStorageKey)
  } catch {
    // Nothing was stored that could be removed.
  }
  apply(detectLang())
  for (const fn of listeners) fn()
}

function subscribe(fn: () => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

/** The current language as React state. */
export function useLang(): Lang {
  return useSyncExternalStore(subscribe, getLang, getLang)
}

function fill(text: string, params: Params | undefined): string {
  if (!params) return text
  return text.replace(/\{(\w+)\}/g, (whole, name: string) =>
    name in params ? String(params[name]) : whole,
  )
}

function pick(entry: Entry, lang: Lang, params: Params | undefined): string {
  if (typeof entry === "string") return fill(entry, params)
  const n = Number(params?.n ?? 0)
  const form = new Intl.PluralRules(lang).select(n)
  return fill(entry[form] ?? entry.other, params)
}

/**
 * Translates a key. A key missing in the current language falls back to English: the test
 * of the dictionaries makes sure that never happens in a release.
 */
export function t(key: Key, params?: Params): string {
  const entry = dictionaries[current][key] ?? en[key]
  return pick(entry as Entry, dictionaries[current][key] ? current : "en", params)
}

/** A date and time in the language of the panel. */
export function formatDateTime(value: Date | string | number): string {
  const d = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(d.getTime())) return "–"
  return d.toLocaleString(current)
}

/** A date in the language of the panel. */
export function formatDate(value: Date | string | number): string {
  const d = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(d.getTime())) return "–"
  return d.toLocaleDateString(current)
}

/** A number in the language of the panel. */
export function formatNumber(n: number): string {
  return new Intl.NumberFormat(current).format(n)
}

const apiErrorKeys = new Map<string, Key>(
  (Object.keys(en) as Key[])
    .filter((k) => k.startsWith("apiError."))
    .map((k) => [en[k] as string, k]),
)

/**
 * The text of an error the server answered. A known English text shows in the current
 * language, anything else keeps the text the server sent.
 */
export function apiErrorText(message: string): string {
  const key = apiErrorKeys.get(message)
  if (key) return t(key)
  const failed = /^Request failed \((\d+)\)$/.exec(message)
  if (failed) return t("apiError.requestFailed", { status: failed[1] ?? "" })
  return message
}

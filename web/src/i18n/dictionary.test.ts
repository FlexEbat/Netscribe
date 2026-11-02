import { describe, expect, it } from "vitest"
import { en } from "@/i18n/en"
import { ru } from "@/i18n/ru"

type Entry = string | Record<string, string>

const texts = (entry: Entry): string[] =>
  typeof entry === "string" ? [entry] : Object.values(entry)
const placeholders = (text: string): string[] =>
  [...text.matchAll(/\{(\w+)\}/g)].map((m) => m[1] ?? "").sort()

describe("the dictionaries", () => {
  it("hold the same keys in English and in Russian", () => {
    const english = Object.keys(en).sort()
    const russian = Object.keys(ru).sort()
    expect(russian.filter((k) => !(k in en))).toEqual([])
    expect(english.filter((k) => !(k in ru))).toEqual([])
  })

  it("give a Russian key the same placeholders as its English key", () => {
    const bad: string[] = []
    for (const key of Object.keys(en) as (keyof typeof en)[]) {
      const english = en[key] as Entry
      const russian = ru[key] as Entry
      if (typeof english === "string") {
        if (
          typeof russian !== "string" ||
          placeholders(russian).join() !== placeholders(english).join()
        ) {
          bad.push(key)
        }
        continue
      }
      if (typeof russian === "string") {
        bad.push(key)
        continue
      }
      // A plural form answers to the English form of the same name, or to "other".
      for (const [form, text] of Object.entries(russian)) {
        const base = english[form] ?? english.other ?? ""
        if (placeholders(text).join() !== placeholders(base).join()) bad.push(`${key}.${form}`)
      }
    }
    expect(bad).toEqual([])
  })

  it("write every plural entry with the forms the language needs", () => {
    const bad: string[] = []
    for (const key of Object.keys(en) as (keyof typeof en)[]) {
      const english = en[key] as Entry
      const russian = ru[key] as Entry
      if (typeof english !== "string" && !("other" in english)) bad.push(`en ${key}`)
      if (typeof russian !== "string") {
        for (const form of ["one", "few", "many", "other"]) {
          if (!(form in russian)) bad.push(`ru ${key}.${form}`)
        }
      }
    }
    expect(bad).toEqual([])
  })

  it("leave no Russian text empty", () => {
    const empty = Object.entries(ru).filter(([, v]) =>
      texts(v as Entry).some((t) => t.trim() === ""),
    )
    expect(empty).toEqual([])
  })
})

describe("the English dictionary", () => {
  const all = Object.entries(en).flatMap(([key, v]) =>
    texts(v as Entry).map((text) => ({ key, text })),
  )

  it("has no long dash", () => {
    expect(all.filter(({ text }) => /[—–]/.test(text))).toEqual([])
  })

  it("has no hyphen inside a word, except the names of files and the word sign-in", () => {
    const allowed = ["before-restore", "sign-in", "Sign-in"]
    const bad = all.filter(({ key, text }) => {
      if (key.startsWith("apiError.")) return false // the server's own words
      const cleaned = allowed.reduce((s, a) => s.split(a).join(""), text)
      return /[A-Za-z]-[A-Za-z]/.test(cleaned)
    })
    expect(bad).toEqual([])
  })

  it("writes short labels in sentence case", () => {
    const names = new Set([
      "API",
      "IP",
      "MAC",
      "Nmap",
      "Docker",
      "Netscribe",
      "GitHub",
      "VLAN",
      "VLANs",
      "CSV",
      "IoT",
      "NAS",
      "OK",
      "PDF",
      "HTML",
      "TLS",
      "OpenAPI",
      "Compose",
      "LAN",
      "Wake",
      "SSH",
      "ssh",
      "Update",
    ])
    const bad = all.filter(({ key, text }) => {
      if (key.startsWith("apiError.") || /[.:?!]/.test(text) || text.includes("{")) return false
      const words = text.split(" ")
      if (words.length > 5) return false
      return words
        .slice(1)
        .some((w) => /^[A-Z][a-z]/.test(w) && !names.has(w.replace(/[….,]+$/, "")))
    })
    expect(bad).toEqual([])
  })
})

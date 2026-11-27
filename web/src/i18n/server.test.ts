import { readFileSync } from "node:fs"
import { join } from "node:path"
import { afterEach, beforeEach, describe, expect, it } from "vitest"
import { getLang, setLang, type Key } from "@/i18n"
import { en } from "@/i18n/en"
import { ru } from "@/i18n/ru"
import { serverRu } from "@/i18n/server.ru"
import { apiGroupName, healthTitle, pluralRu, serverText } from "@/i18n/server"

const root = join(__dirname, "..", "..", "..")
const read = (path: string) => readFileSync(join(root, path), "utf8")

/** The text of every Go string literal that matches the pattern, unescaped. */
function literals(code: string, re: RegExp): string[] {
  const out: string[] = []
  for (const m of code.matchAll(re)) {
    const raw = m[1] ?? m[2] ?? ""
    out.push(m[2] !== undefined ? raw : (JSON.parse(`"${raw}"`) as string))
  }
  return out
}

const quoted = '"((?:[^"\\\\]|\\\\.)*)"'

describe("the Russian text of the sentences the server writes", () => {
  it("covers the settings catalog", () => {
    const whole = read("internal/setup/settings.go")
    const code = whole.slice(whole.indexOf("var settingGroups"), whole.indexOf("func findSetting"))
    const all = [
      ...literals(code, new RegExp(`(?:label|help|name|hint): ${quoted}`, "g")),
      ...literals(code, new RegExp(`help: ${quoted}\\s*\\+`, "g")),
    ].filter((s) => /[A-Za-z]{3}/.test(s) && !/^[A-Za-z0-9_]{1,6}$/.test(s))
    const missing = all.filter((s) => serverRu[s] === undefined)
    expect(missing).toEqual([])
  })

  it("has a note and a reason in both dictionaries for every risk rule", () => {
    const code = read("internal/model/risk.go")
    const rules = [
      ...code.matchAll(
        new RegExp(`Risk\\{Risk(?:High|Medium), ${quoted}, ${quoted}, ${quoted}\\}`, "g"),
      ),
    ]
    expect(rules.length).toBeGreaterThan(20)
    for (const m of rules) {
      const [message, id, why] = [m[1], m[2], m[3]].map((x) => JSON.parse(`"${x}"`) as string)
      expect(en[`risk.${id}` as Key], `en risk.${id}`).toBe(message)
      expect(en[`riskWhy.${id}` as Key], `en riskWhy.${id}`).toBe(why)
      expect(ru[`risk.${id}` as Key], `ru risk.${id}`).toBeTruthy()
      expect(ru[`riskWhy.${id}` as Key], `ru riskWhy.${id}`).toBeTruthy()
    }
  })

  it("covers the fixed sentences of the health checks", () => {
    const code = read("internal/health/health.go")
    const all = literals(code, new RegExp(`(?:Detail|Fix): ${quoted}`, "g")).concat(
      literals(code, new RegExp(`ok\\(${quoted}\\)`, "g")),
    )
    const missing = all.filter((s) => !/%|^sudo | $/.test(s) && serverRu[s] === undefined)
    expect(missing).toEqual([])
  })

  it("covers the API reference", () => {
    const code = read("internal/api/routedocs.go")
    const docs = [
      ...literals(code, new RegExp(`(?:Summary|Body|Answer): ${quoted}`, "g")),
      ...[...code.matchAll(/(?:Summary|Body|Answer): `([^`]*)`/g)].map((m) => m[1] ?? ""),
    ]
    for (const q of code.matchAll(/Query: \[\]string\{([^}]*)\}/g)) {
      for (const s of literals(q[1] ?? "", new RegExp(quoted, "g")))
        docs.push(s.replace(/^[^:]+: /, ""))
    }
    const prose = docs.filter((s) => /[a-z]{3} [a-z]{2}|^[A-Z][a-z]+\.$/.test(s))
    expect(prose.filter((s) => serverRu[s] === undefined)).toEqual([])
  })
})

describe("serverText", () => {
  beforeEach(() => setLang("ru"))
  afterEach(() => setLang("en"))

  it("leaves English as the server wrote it", () => {
    setLang("en")
    expect(serverText("a scan is running, or scheduled scans are off")).toBe(
      "a scan is running, or scheduled scans are off",
    )
    expect(getLang()).toBe("en")
  })

  it("translates the change feed with the names kept", () => {
    expect(serverText("New device nas-1 (192.168.1.5, router)")).toBe(
      "Новое устройство nas-1 (192.168.1.5, Маршрутизатор)",
    )
  })

  it("translates a device change field by field", () => {
    expect(serverText("nas-1 changed: ip 1.1.1.1 -> 1.1.1.2; hostname - -> nas")).toBe(
      "nas-1: изменилось: IP 1.1.1.1 -> 1.1.1.2; имя хоста - -> nas",
    )
  })

  it("translates certificate and container lines", () => {
    expect(serverText("Certificate of web port 443 ends 2027-01-02")).toBe(
      "Сертификат web, порт 443 истекает 2027-01-02",
    )
    expect(serverText("Container db (postgres) is gone from host")).toBe(
      "Контейнер db (postgres) исчез с host",
    )
  })

  it("uses the right plural of days", () => {
    expect(serverText("the certificate expires in 1 days")).toBe("сертификат истекает через 1 день")
    expect(serverText("the certificate expires in 3 days")).toBe("сертификат истекает через 3 дня")
    expect(serverText("the certificate expires in 11 days")).toBe(
      "сертификат истекает через 11 дней",
    )
    expect(serverText("the certificate is valid for 21 more days")).toBe(
      "сертификат действителен ещё 21 день",
    )
    expect(serverText("the certificate is valid for 21 more days, self-signed")).toBe(
      "сертификат действителен ещё 21 день, самоподписанный",
    )
    expect([1, 2, 5, 11, 21, 22].map((n) => pluralRu(n, "a", "b", "c"))).toEqual([
      "a",
      "b",
      "c",
      "c",
      "a",
      "b",
    ])
  })

  it("translates the sentences about what a route needs", () => {
    expect(serverText("Needs the right devices.read. An access token may call it.")).toBe(
      "Нужно право devices.read. Токен доступа может его вызывать.",
    )
    expect(serverText("Needs a signed in session. Not available to access tokens.")).toBe(
      "Нужна сессия с выполненным входом. Недоступно для токенов доступа.",
    )
  })

  it("keeps a sentence it does not know", () => {
    expect(serverText("something new the server says")).toBe("something new the server says")
  })

  it("names health checks and route groups", () => {
    expect(healthTitle("disk.free", "Free disk space")).toBe("Свободное место на диске")
    expect(healthTitle("unknown.id", "Whatever")).toBe("Whatever")
    expect(apiGroupName("devices")).toBe("Устройства")
  })
})

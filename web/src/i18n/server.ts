import { getLang, hasKey, t } from "@/i18n"
import { serverRu } from "@/i18n/server.ru"

/*
 * Some sentences the panel shows are written by the server, in English: the notes of the
 * risk rules, the health checks, the settings catalog, the change feed, the reasons of the
 * classifier and the API reference. The server keeps one language so that logs, exports
 * and notifications read the same everywhere; the panel puts these sentences into the
 * language of the person.
 *
 * serverText() looks the exact sentence up in server.ru.ts, then tries the patterns below
 * for sentences that carry a name or a number. A sentence it does not know is shown as the
 * server wrote it, and server.test.ts lists every sentence of the Go sources that has no
 * Russian text, so a new sentence cannot slip in unnoticed.
 */

type Rule = [RegExp, (m: RegExpExecArray) => string]

/** The Russian plural form of a number: one, few or many. */
export function pluralRu(n: number, one: string, few: string, many: string): string {
  const m10 = n % 10
  const m100 = n % 100
  if (m10 === 1 && m100 !== 11) return one
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return few
  return many
}

/** The name of a device kind in the current language; an unknown kind stays as it is. */
function kindName(kind: string): string {
  const key = `kind.${kind}`
  return hasKey(key) ? t(key) : kind
}

const fieldNames: Record<string, string> = {
  ip: "IP",
  ip6: "IPv6",
  hostname: "имя хоста",
  kind: "тип",
  system: "система",
}

/** One part of "a device changed: ip 1.2.3.4 -> 1.2.3.5; kind host -> router". */
function changePart(part: string): string {
  const m = /^(ip6|ip|hostname|kind|system) (.+) -> (.+)$/.exec(part)
  if (!m) return part
  const field = m[1] ?? ""
  const value = (v: string) => (field === "kind" ? kindName(v) : v)
  return `${fieldNames[field] ?? field} ${value(m[2] ?? "")} -> ${value(m[3] ?? "")}`
}

/** "web port 443" becomes "web, порт 443". */
function where(text: string): string {
  const m = /^(.+) port (\d+)$/.exec(text)
  return m ? `${m[1]}, порт ${m[2]}` : text
}

const daysLeft = (n: number) => `${n} ${pluralRu(n, "день", "дня", "дней")}`

const csvLine: Rule[] = [
  [
    /^"(.*)" is not a column, use label, ip, ip6, mac, hostname, kind, group, notes, owner, location, serial, warranty or tags$/,
    (m) => `«${m[1]}» не столбец, используйте label, ip, ip6, mac, hostname, kind, group, notes, owner, location, serial, warranty или tags`,
  ],
  [/^the column "(.*)" is listed twice$/, (m) => `столбец «${m[1]}» указан дважды`],
  [
    /^the row has a different number of cells than the header$/,
    () => "в строке другое число ячеек, чем в заголовке",
  ],
  [
    /^the file is not valid CSV \(a quote is not closed or misplaced\)$/,
    () => "файл не является корректным CSV (кавычка не закрыта или стоит не на месте)",
  ],
]

const rules: Rule[] = [
  // The change feed.
  [
    /^New device (.+) \((.*), (\w+)\)$/,
    (m) => `Новое устройство ${m[1]} (${m[2]}, ${kindName(m[3] ?? "")})`,
  ],
  [/^(.+) is back online$/, (m) => `${m[1]}: снова в сети`],
  [/^(.+) went offline$/, (m) => `${m[1]}: пропало из сети`],
  [
    /^Certificate of (.+) changed: (.+) -> (.+)$/,
    (m) => `Сертификат ${where(m[1] ?? "")} изменился: ${m[2]} -> ${m[3]}`,
  ],
  [
    /^Certificate of (.+) ends (\d{4}-\d{2}-\d{2})$/,
    (m) => `Сертификат ${where(m[1] ?? "")} истекает ${m[2]}`,
  ],
  [/^Link appeared: (.+)$/, (m) => `Появилась связь: ${m[1]}`],
  [/^Link disappeared: (.+)$/, (m) => `Связь пропала: ${m[1]}`],
  [
    /^Container (.+) \((.*)\) appeared on (.+)$/,
    (m) => `Контейнер ${m[1]} (${m[2]}) появился на ${m[3]}`,
  ],
  [
    /^Container (.+) \((.*)\) is gone from (.+)$/,
    (m) => `Контейнер ${m[1]} (${m[2]}) исчез с ${m[3]}`,
  ],
  [
    /^(.+) changed: (.+)$/,
    (m) => `${m[1]}: изменилось: ${(m[2] ?? "").split("; ").map(changePart).join("; ")}`,
  ],

  // The reasons of the classifier.
  [/^sysDescr: (.+)$/, (m) => `описание системы (sysDescr): ${m[1]}`],
  [/^vendor: (.+)$/, (m) => `производитель: ${m[1]}`],
  [/^HTTP banner: (.+)$/, (m) => `HTTP-баннер: ${m[1]}`],
  [/^TLS subject: (.+)$/, (m) => `субъект TLS: ${m[1]}`],
  [/^SNMP services: (.+)$/, (m) => `службы SNMP: ${m[1]}`],
  [/^port (\d+)$/, (m) => `порт ${m[1]}`],
  [/^candidate (\w+) \((\d+)\)$/, (m) => `кандидат: ${kindName(m[1] ?? "")} (${m[2]})`],

  // The health checks.
  [
    /^Check that the service account can read (.+)\.$/,
    (m) => `Проверьте, что учётная запись службы может прочитать ${m[1]}.`,
  ],
  [
    /^(\d+) MiB free where the database lives: writes will soon fail$/,
    (m) => `${m[1]} МиБ свободно там, где лежит база: запись скоро начнёт падать`,
  ],
  [/^(\d+) MiB free where the database lives$/, (m) => `${m[1]} МиБ свободно там, где лежит база`],
  [
    /^(\d+) networks are set$/,
    (m) => {
      const n = Number(m[1])
      return `${pluralRu(n, "задана", "заданы", "задано")} ${n} ${pluralRu(n, "сеть", "сети", "сетей")}`
    },
  ],
  [
    /^the latest scan was (.+) ago, and one runs every (.+)$/,
    (m) => `последнее сканирование было ${m[1]} назад, а запуск идёт каждые ${m[2]}`,
  ],
  [/^a browser was found: (.+)$/, (m) => `найден браузер: ${m[1]}`],
  [
    /^the certificate expired on (\d{4}-\d{2}-\d{2})$/,
    (m) => `срок действия сертификата истёк ${m[1]}`,
  ],
  [
    /^the certificate expires in (\d+) days$/,
    (m) => `сертификат истекает через ${daysLeft(Number(m[1]))}`,
  ],
  [
    /^the certificate is valid for (\d+) more days(, self-signed)?$/,
    (m) =>
      `сертификат действителен ещё ${daysLeft(Number(m[1]))}${m[2] ? ", самоподписанный" : ""}`,
  ],

  // The messages about a file of devices.
  [/^line (\d+): (.+)$/, (m) => `строка ${m[1]}: ${translateWith(csvLine, m[2] ?? "")}`],
  [/^a file may have at most (\d+) rows$/, (m) => `в файле может быть не больше ${m[1]} строк`],
  [/^reading the file: (.+)$/, (m) => `чтение файла: ${m[1]}`],

  // The messages of the update helper.
  [/^the service still runs (.+)$/, (m) => `служба всё ещё работает на версии ${m[1]}`],

  // The sentences that tell what a route needs, in the API reference.
  [
    /^Needs the right (.+?)\.( An access token may call it\.| Not available to access tokens\.)?$/,
    (m) => `Нужно право ${m[1]}.${tokenNote(m[2])}`,
  ],
  [
    /^Needs a signed in session\.( An access token may call it\.| Not available to access tokens\.)?$/,
    (m) => `Нужна сессия с выполненным входом.${tokenNote(m[1])}`,
  ],
  [/^Needs no sign in\.$/, () => "Вход не нужен."],
]

function tokenNote(note: string | undefined): string {
  if (!note) return ""
  return note.includes("may call")
    ? " Токен доступа может его вызывать."
    : " Недоступно для токенов доступа."
}

function translateWith(list: Rule[], text: string): string {
  for (const [re, make] of list) {
    const m = re.exec(text)
    if (m) return make(m)
  }
  return text
}

/**
 * A sentence the server wrote, in the language of the panel. In English, and for a sentence
 * that has no Russian text, it is returned as it came.
 */
export function serverText(text: string): string {
  if (getLang() !== "ru" || !text) return text
  const known = serverRu[text]
  if (known !== undefined) return known
  return translateWith(rules, text)
}

const healthTitles: Record<string, string> = {
  "config.mode": "Права на файл конфигурации",
  "disk.free": "Свободное место на диске",
  "scan.targets": "Сети для сканирования",
  "scan.recent": "Последнее сканирование",
  nmap: "Nmap",
  "export.browser": "Браузер для экспорта в PDF",
  "docker.socket": "Сокет Docker",
  "tls.cert": "TLS-сертификат",
  "update.helper": "Обновления из панели",
}

/** The title of a health check, by the id the server gives it. */
export function healthTitle(id: string, title: string): string {
  if (getLang() !== "ru") return title
  return healthTitles[id] ?? title
}

const apiGroups: Record<string, string> = {
  audit: "Аудит",
  auth: "Вход и сессии",
  backup: "Резервные копии",
  changes: "Изменения",
  containers: "Контейнеры",
  devices: "Устройства",
  events: "События",
  export: "Экспорт",
  filters: "Фильтры",
  groups: "Группы",
  health: "Состояние",
  links: "Связи",
  nmap: "Nmap",
  notifications: "Уведомления",
  "openapi.json": "Описание OpenAPI",
  overview: "Обзор",
  scans: "Сканирования",
  service: "Служебные",
  services: "Службы",
  settings: "Настройки",
  system: "Система",
  tokens: "Токены",
  topology: "Топология",
  update: "Обновления",
  users: "Пользователи",
  version: "Версия",
}

/** The name of a group of routes in the API reference. */
export function apiGroupName(key: string): string {
  if (getLang() !== "ru") return key
  return apiGroups[key] ?? key
}

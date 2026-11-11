import { useCallback, useEffect, useId, useMemo, useState } from "react"
import {
  ApiError,
  type AutoUpdateStatus,
  checkForUpdate,
  getSettings,
  getVersion,
  restartService,
  resumeAutoUpdates,
  saveSettings,
  type Setting,
  type SettingsGroup,
  type SettingsSaved,
  type VersionInfo,
} from "@/api/client"
import { BackupCard } from "@/components/BackupCard"
import { NotificationsCard } from "@/components/NotificationsCard"
import { UpdateDialog } from "@/components/UpdateDialog"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { formatDateTime, t, type Key } from "@/i18n"
import { tRich } from "@/i18n/rich"
import { PageTitle } from "@/components/PageTitle"

/** What the person typed or chose for each key. A key stays out until it is touched. */
type Draft = Record<string, string | boolean>

/** The text a person edits for a setting. Lists are written with commas. */
function shown(s: Setting): string | boolean {
  switch (s.type) {
    case "bool":
      return s.value === true
    case "list":
    case "ports":
      return Array.isArray(s.value) ? s.value.join(", ") : ""
    case "secrets":
      return ""
    default:
      return s.value === null || s.value === undefined ? "" : String(s.value)
  }
}

/** Turns what was typed into the value the server expects. */
function toValue(s: Setting, v: string | boolean): unknown {
  switch (s.type) {
    case "bool":
      return v === true
    case "int":
      return Number(String(v).trim())
    case "list":
    case "ports":
    case "secrets":
      return String(v)
        .split(/[,\s]+/)
        .filter((x) => x !== "")
    default:
      return String(v)
  }
}

function SettingRow({
  setting,
  value,
  onChange,
}: {
  setting: Setting
  value: string | boolean
  onChange: (v: string | boolean) => void
}) {
  const id = useId()
  const hint =
    `${setting.help} ${t(setting.hot ? "settings.appliesAtOnce" : "settings.appliesAfterRestart")}`.trim()
  const field = (() => {
    switch (setting.type) {
      case "bool":
        return (
          <label className="flex items-center gap-2 text-sm">
            <input
              id={id}
              type="checkbox"
              className="size-4 accent-[var(--brand)]"
              checked={value === true}
              onChange={(e) => onChange(e.target.checked)}
            />
            {t(value === true ? "settings.on" : "settings.off")}
          </label>
        )
      case "enum":
        return (
          <select
            id={id}
            value={String(value)}
            onChange={(e) => onChange(e.target.value)}
            className="h-9 rounded-sm border bg-background px-3 text-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
          >
            {(setting.choices ?? []).map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </select>
        )
      case "secrets":
        return (
          <Input
            id={id}
            type="password"
            autoComplete="off"
            value={String(value)}
            placeholder={t("settings.secretPlaceholder", { n: setting.count ?? 0 })}
            onChange={(e) => onChange(e.target.value)}
          />
        )
      case "int":
        return (
          <Input
            id={id}
            type="number"
            inputMode="numeric"
            value={String(value)}
            onChange={(e) => onChange(e.target.value)}
          />
        )
      default:
        return (
          <Input
            id={id}
            value={String(value)}
            placeholder={setting.default}
            onChange={(e) => onChange(e.target.value)}
          />
        )
    }
  })()
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{setting.label}</Label>
      {field}
      <p className="text-xs text-muted-foreground">{hint}</p>
    </div>
  )
}

export function Settings() {
  const [groups, setGroups] = useState<SettingsGroup[] | null>(null)
  const [draft, setDraft] = useState<Draft>({})
  const [error, setError] = useState("")
  const [loadError, setLoadError] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState<SettingsSaved | null>(null)
  const [info, setInfo] = useState<VersionInfo | null>(null)
  const [dialog, setDialog] = useState(false)
  const [note, setNote] = useState("")

  const load = useCallback(() => {
    getSettings()
      .then((g) => {
        setGroups(g)
        setDraft({})
      })
      .catch(() => setLoadError(true))
  }, [])
  useEffect(() => {
    load()
    getVersion()
      .then(setInfo)
      .catch(() => undefined)
  }, [load])

  const all = useMemo(() => (groups ?? []).flatMap((g) => g.settings), [groups])
  const changes = useMemo(() => {
    const out: Record<string, unknown> = {}
    for (const s of all) {
      const v = draft[s.key]
      if (v === undefined) continue
      if (s.type === "secrets" && v === "") continue
      out[s.key] = toValue(s, v)
    }
    return out
  }, [all, draft])
  const dirty = Object.keys(changes).length > 0

  async function save() {
    setError("")
    setSaved(null)
    setSaving(true)
    try {
      setSaved(await saveSettings(changes))
      load()
    } catch (e) {
      setError(
        e instanceof ApiError && e.status !== 0 && e.status < 500
          ? e.message
          : t("settings.notSaved"),
      )
    } finally {
      setSaving(false)
    }
  }

  async function recheck() {
    setNote("")
    try {
      const next = await checkForUpdate()
      setInfo(next)
      if (next.update.error) setNote(t("settings.githubUnreachable", { error: next.update.error }))
      else if (!next.update.available) setNote(t("settings.newest"))
    } catch (e) {
      setNote(e instanceof ApiError && e.status === 409 ? e.message : t("settings.checkFailed"))
    }
  }

  async function restart() {
    setError("")
    try {
      await restartService()
      setSaved(null)
      setNote(t("settings.restarting"))
    } catch {
      setError(t("settings.restartFailed"))
    }
  }

  if (loadError) return <Alert variant="destructive">{t("settings.theSettingsCouldNotBe")}</Alert>
  if (!groups) {
    return (
      <div
        role="status"
        aria-label={t("settings.loadingSettings")}
        className="flex max-w-3xl flex-col gap-4"
      >
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-48 w-full" />
        <Skeleton className="h-48 w-full" />
      </div>
    )
  }

  return (
    <div className="flex max-w-3xl flex-col gap-6">
      <PageTitle page="settings">{t("settings.settings")}</PageTitle>
      <p className="text-sm text-muted-foreground">
        {tRich("settings.intro", {
          file: <code className="font-mono">{t("settings.netscribeConfig")}</code>,
        })}
      </p>

      {info ? (
        <Card>
          <CardHeader>
            <CardTitle>{t("settings.version")}</CardTitle>
            <CardDescription>
              {info.update.latest
                ? t("settings.runningNewest", {
                    version: info.version,
                    tag: info.update.latest.tag,
                  })
                : t("settings.running", { version: info.version })}
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-wrap items-center gap-3">
            <Button variant="outline" onClick={() => void recheck()}>
              {t("settings.checkForUpdates")}
            </Button>
            {info.update.available ? (
              <Button onClick={() => setDialog(true)}>
                {t("settings.updateTo", { tag: info.update.latest?.tag ?? "" })}
              </Button>
            ) : null}
            {note ? (
              <span role="status" className="text-sm text-muted-foreground">
                {note}
              </span>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      {info?.auto ? <AutoUpdatesCard auto={info.auto} onInfo={setInfo} /> : null}

      {groups.map((g) => (
        <Card key={g.name}>
          <CardHeader>
            <CardTitle>{g.name}</CardTitle>
            <CardDescription>{g.hint}</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-6">
            {g.settings.map((s) => (
              <SettingRow
                key={s.key}
                setting={s}
                value={draft[s.key] ?? shown(s)}
                onChange={(v) => setDraft((d) => ({ ...d, [s.key]: v }))}
              />
            ))}
          </CardContent>
        </Card>
      ))}

      <NotificationsCard />

      <BackupCard />

      {error ? <Alert variant="destructive">{error}</Alert> : null}
      {saved ? (
        <Alert>
          {t("settings.savedN", { n: saved.saved.length })}
          {saved.needRestart.length > 0 ? (
            <>
              {" "}
              {t("settings.needRestart", { list: saved.needRestart.join(", ") })}{" "}
              {info?.helper.available ? (
                <Button variant="outline" size="sm" onClick={() => void restart()}>
                  {t("settings.restartNow")}
                </Button>
              ) : (
                tRich("settings.runCommand", {
                  cmd: (
                    <code className="font-mono">{t("settings.sudoSystemctlRestartNetscribe")}</code>
                  ),
                })
              )}
            </>
          ) : null}
        </Alert>
      ) : null}
      <div className="flex gap-2">
        <Button disabled={!dirty || saving} onClick={() => void save()}>
          {t("settings.saveChanges")}
        </Button>
        <Button variant="outline" disabled={!dirty || saving} onClick={() => setDraft({})}>
          {t("settings.discard")}
        </Button>
      </div>

      {dialog && info?.update.latest ? (
        <UpdateDialog info={info} canUpdate onClose={() => setDialog(false)} onInfo={setInfo} />
      ) : null}
    </div>
  )
}

const autoResults = {
  requested: "settings.autoRequested",
  ok: "settings.autoOk",
  failed: "settings.autoFailed",
  "rolled back": "settings.autoRolledBack",
} as const satisfies Record<string, Key>

/** What the service did about automatic updates, and the way back after a rollback. */
function AutoUpdatesCard({
  auto,
  onInfo,
}: {
  auto: AutoUpdateStatus
  onInfo: (info: VersionInfo) => void
}) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  async function resume() {
    setBusy(true)
    setError("")
    try {
      onInfo(await resumeAutoUpdates())
    } catch (e) {
      setError(e instanceof ApiError ? e.message : t("settings.autoResumeFailed"))
    } finally {
      setBusy(false)
    }
  }

  const result = auto.last
    ? (autoResults[auto.last.result as keyof typeof autoResults] ?? null)
    : null
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("settings.autoUpdates")}</CardTitle>
        <CardDescription>
          {auto.mode === "off"
            ? t("settings.autoOffText")
            : t("settings.autoOnText", { mode: auto.mode, window: auto.window })}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        {auto.paused ? (
          <Alert variant="destructive">
            <p>
              {t("settings.autoPaused", {
                reason: auto.pausedReason ?? "",
              })}
            </p>
            <Button
              variant="outline"
              className="mt-2"
              disabled={busy}
              onClick={() => void resume()}
            >
              {t("settings.autoResume")}
            </Button>
          </Alert>
        ) : null}
        {error ? <Alert variant="destructive">{error}</Alert> : null}
        {auto.last ? (
          <p>
            {t("settings.autoLast", {
              tag: auto.last.tag,
              date: formatDateTime(auto.last.at),
              result: result ? t(result) : auto.last.result,
            })}
            {auto.last.message ? ` ${auto.last.message}` : ""}
          </p>
        ) : (
          <p className="text-muted-foreground">{t("settings.autoNone")}</p>
        )}
      </CardContent>
    </Card>
  )
}

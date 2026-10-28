import { useCallback, useEffect, useId, useMemo, useState } from "react"
import {
  ApiError,
  checkForUpdate,
  getSettings,
  getVersion,
  restartService,
  saveSettings,
  type Setting,
  type SettingsGroup,
  type SettingsSaved,
  type VersionInfo,
} from "@/api/client"
import { UpdateDialog } from "@/components/UpdateDialog"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

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
    `${setting.help} ${setting.hot ? "Applies at once." : "Applies after a restart."}`.trim()
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
            {value === true ? "On" : "Off"}
          </label>
        )
      case "enum":
        return (
          <select
            id={id}
            value={String(value)}
            onChange={(e) => onChange(e.target.value)}
            className="h-9 rounded-xl border bg-background px-3 text-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
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
            placeholder={`${setting.count ?? 0} set. Type new ones to replace all, separated by commas`}
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
    <div className="flex flex-col gap-1.5">
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
          : "The settings were not saved. Try again later.",
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
      if (next.update.error) setNote("GitHub could not be reached: " + next.update.error)
      else if (!next.update.available) setNote("This is the newest release.")
    } catch (e) {
      setNote(e instanceof ApiError && e.status === 409 ? e.message : "The check failed.")
    }
  }

  async function restart() {
    setError("")
    try {
      await restartService()
      setSaved(null)
      setNote("Restarting. The panel is back in a few seconds.")
    } catch {
      setError("The restart could not be started.")
    }
  }

  if (loadError) return <Alert variant="destructive">The settings could not be loaded.</Alert>
  if (!groups) return <p className="text-sm text-muted-foreground">Loading…</p>

  return (
    <div className="flex max-w-3xl flex-col gap-6">
      <h1 className="text-2xl font-semibold">Settings</h1>
      <p className="text-sm text-muted-foreground">
        The same settings as <code className="font-mono">netscribe config</code>. The address the
        panel listens on, TLS and trusted proxies are changed only on the server, so a mistake here
        cannot lock you out.
      </p>

      {info ? (
        <Card>
          <CardHeader>
            <CardTitle>Version</CardTitle>
            <CardDescription>
              Running {info.version}
              {info.update.latest ? `, newest release ${info.update.latest.tag}` : ""}.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-wrap items-center gap-3">
            <Button variant="outline" onClick={() => void recheck()}>
              Check for updates
            </Button>
            {info.update.available ? (
              <Button onClick={() => setDialog(true)}>Update to {info.update.latest?.tag}</Button>
            ) : null}
            {note ? (
              <span role="status" className="text-sm text-muted-foreground">
                {note}
              </span>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      {groups.map((g) => (
        <Card key={g.name}>
          <CardHeader>
            <CardTitle>{g.name}</CardTitle>
            <CardDescription>{g.hint}</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-5">
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

      {error ? <Alert variant="destructive">{error}</Alert> : null}
      {saved ? (
        <Alert>
          Saved {saved.saved.length} {saved.saved.length === 1 ? "setting" : "settings"}.
          {saved.needRestart.length > 0 ? (
            <>
              {" "}
              These need a restart to apply: {saved.needRestart.join(", ")}.{" "}
              {info?.helper.available ? (
                <Button variant="outline" size="sm" onClick={() => void restart()}>
                  Restart now
                </Button>
              ) : (
                <>
                  Run <code className="font-mono">sudo systemctl restart netscribe</code>.
                </>
              )}
            </>
          ) : null}
        </Alert>
      ) : null}
      <div className="flex gap-2">
        <Button disabled={!dirty || saving} onClick={() => void save()}>
          Save changes
        </Button>
        <Button variant="outline" disabled={!dirty || saving} onClick={() => setDraft({})}>
          Discard
        </Button>
      </div>

      {dialog && info?.update.latest ? (
        <UpdateDialog info={info} canUpdate onClose={() => setDialog(false)} onInfo={setInfo} />
      ) : null}
    </div>
  )
}

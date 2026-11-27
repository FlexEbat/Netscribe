import { useEffect, useState } from "react"
import {
  ApiError,
  listNotifications,
  testNotification,
  type NotificationChannel,
  type NotificationRules,
} from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { t } from "@/i18n"
import { tRich } from "@/i18n/rich"

type State =
  | { status: "loading" }
  | { status: "failed" }
  | { status: "ready"; channels: NotificationChannel[]; rules: NotificationRules | null }

/** What the answer to "Send test" tells a person. */
function testMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 429) return t("notificationsCard.testRecent")
    if (error.status === 404) return t("notificationsCard.channelGone")
    if (error.status === 502) return error.message
  }
  return t("notificationsCard.testFailed")
}

/** The channels of the config, read only: they hold secrets, so they are edited in the file. */
export function NotificationsCard() {
  const [state, setState] = useState<State>({ status: "loading" })
  const [results, setResults] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState("")

  useEffect(() => {
    let cancelled = false
    listNotifications()
      .then(({ channels, rules }) => !cancelled && setState({ status: "ready", channels, rules }))
      .catch(() => !cancelled && setState({ status: "failed" }))
    return () => {
      cancelled = true
    }
  }, [])

  async function send(name: string) {
    setBusy(name)
    try {
      await testNotification(name)
      setResults((r) => ({ ...r, [name]: t("notificationsCard.testSent") }))
    } catch (error) {
      setResults((r) => ({ ...r, [name]: testMessage(error) }))
    } finally {
      setBusy("")
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("notificationsCard.notifications")}</CardTitle>
        <CardDescription>
          {tRich("notificationsCard.description", {
            notify: <code className="font-mono">{t("notificationsCard.notify")}</code>,
          })}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {state.status === "loading" ? (
          <p role="status" className="text-sm text-muted-foreground">
            {t("notificationsCard.loadingTheChannels")}
          </p>
        ) : null}
        {state.status === "failed" ? (
          <Alert variant="destructive">{t("notificationsCard.theChannelsCouldNotBe")}</Alert>
        ) : null}
        {state.status === "ready" && state.channels.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            {t("notificationsCard.noChannelIsConfigured")}
          </p>
        ) : null}
        {state.status === "ready"
          ? state.channels.map((c) => (
              <div key={c.name} className="flex flex-wrap items-center gap-3">
                <span className="font-medium">{c.name}</span>
                <Badge variant="outline">{c.type}</Badge>
                <span className="text-sm text-muted-foreground">
                  {c.events.length > 0 ? c.events.join(", ") : t("notificationsCard.noEvents")}
                </span>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={busy !== ""}
                  aria-label={t("notificationsCard.sendTestTo", { name: c.name })}
                  onClick={() => void send(c.name)}
                >
                  {t("notificationsCard.sendTest")}
                </Button>
                {results[c.name] ? (
                  <span role="status" className="text-sm text-muted-foreground">
                    {results[c.name]}
                  </span>
                ) : null}
              </div>
            ))
          : null}
        {state.status === "ready" ? <Rules rules={state.rules} /> : null}
      </CardContent>
    </Card>
  )
}

/** Flapping, quiet hours and maintenance windows, read only: they are edited in the file. */
function Rules({ rules }: { rules: NotificationRules | null }) {
  // The clock is read once when the card opens: a window is "over" by that time.
  const [now] = useState(() => Date.now())
  if (!rules) return null
  return (
    <div className="flex flex-col gap-1 border-t pt-4 text-sm">
      <h3 className="font-medium">{t("notificationsCard.steadyAlerts")}</h3>
      <p className="text-muted-foreground">
        {t("notificationsCard.flapRule", { count: String(rules.flapCount) })}
      </p>
      <p className="text-muted-foreground">
        {rules.quietHours
          ? t("notificationsCard.quietHoursAre", { range: rules.quietHours })
          : t("notificationsCard.noQuietHours")}
      </p>
      {rules.maintenance.length === 0 ? (
        <p className="text-muted-foreground">{t("notificationsCard.noMaintenance")}</p>
      ) : (
        <ul className="text-muted-foreground">
          {rules.maintenance.map((m) => (
            <li key={`${m.from}|${m.to}|${m.group}`}>
              {t("notificationsCard.window", {
                from: m.from.replace("T", " "),
                to: m.to.replace("T", " "),
                group: m.group || t("notificationsCard.wholeNetwork"),
              })}
              {Date.parse(m.to) < now ? ` (${t("notificationsCard.windowOver")})` : ""}
            </li>
          ))}
        </ul>
      )}
      <p className="text-muted-foreground text-xs">{t("notificationsCard.editInTheFile")}</p>
    </div>
  )
}

import { useEffect, useState } from "react"
import { endOtherSessions, endSession, listSessions, type SessionInfo } from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { formatDateTime, t } from "@/i18n"

/** Where the account is signed in, with a way to end each of them. */
export function SessionsCard() {
  const [sessions, setSessions] = useState<SessionInfo[] | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  const [version, setVersion] = useState(0)

  useEffect(() => {
    let live = true
    listSessions()
      .then((list) => {
        if (!live) return
        setSessions(list)
        setError("")
      })
      .catch(() => live && setError(t("sessionsCard.couldNotLoad")))
    return () => {
      live = false
    }
  }, [version])

  async function act(run: () => Promise<unknown>) {
    setBusy(true)
    try {
      await run()
      setVersion((v) => v + 1)
    } catch {
      setError(t("sessionsCard.didNotWork"))
    } finally {
      setBusy(false)
    }
  }

  const others = (sessions ?? []).filter((s) => !s.current).length
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("sessionsCard.sessions")}</CardTitle>
        <CardDescription>{t("sessionsCard.whereYourAccountIsSigned")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {error ? <Alert variant="destructive">{error}</Alert> : null}
        <ul className="flex flex-col gap-3">
          {(sessions ?? []).map((s) => (
            <li
              key={s.id}
              className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3"
            >
              <div className="flex flex-col gap-1 text-sm">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{s.ip || t("sessionsCard.unknownAddress")}</span>
                  {s.current ? <Badge>{t("sessionsCard.thisSession")}</Badge> : null}
                </div>
                <span className="text-muted-foreground">
                  {s.userAgent || t("sessionsCard.unknownBrowser")}
                </span>
                <span className="text-muted-foreground">
                  {t("sessionsCard.signedIn", {
                    created: formatDateTime(s.createdAt),
                    last: formatDateTime(s.lastSeenAt),
                  })}
                </span>
              </div>
              {s.current ? null : (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={busy}
                  aria-label={t("sessionsCard.endFrom", {
                    address: s.ip || t("sessionsCard.anUnknownAddress"),
                  })}
                  onClick={() => void act(() => endSession(s.id))}
                >
                  {t("sessionsCard.end")}
                </Button>
              )}
            </li>
          ))}
        </ul>
        {others > 0 ? (
          <div>
            <Button variant="outline" disabled={busy} onClick={() => void act(endOtherSessions)}>
              {t("sessionsCard.endAllOtherSessions")}
            </Button>
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}

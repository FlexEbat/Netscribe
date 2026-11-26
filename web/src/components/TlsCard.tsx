import { useEffect, useState } from "react"
import {
  ApiError,
  getTls,
  makeSelfSigned,
  restartService,
  type TlsResult,
  type TlsStatus,
} from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { formatDateTime, t } from "@/i18n"
import { tRich } from "@/i18n/rich"

/** What a refused request tells a person. */
function tlsMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 403) return t("tlsCard.passwordNotRight")
    if (error.status === 429) return t("tlsCard.tooManyPasswords")
    if ([400, 409].includes(error.status)) return error.message
  }
  return t("tlsCard.didNotWork")
}

/**
 * The https certificate of the panel: what it serves now, and a way to make a self-signed one.
 * The private key never reaches the browser; the card shows the fingerprint to compare with
 * the warning a browser gives for a certificate it does not know.
 */
export function TlsCard({ canRestart }: { canRestart: boolean }) {
  const [status, setStatus] = useState<TlsStatus | null>(null)
  const [loadFailed, setLoadFailed] = useState(false)
  const [names, setNames] = useState(() => window.location.hostname)
  const [days, setDays] = useState("365")
  const [password, setPassword] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [made, setMade] = useState<TlsResult | null>(null)
  const [restarted, setRestarted] = useState(false)

  useEffect(() => {
    let live = true
    getTls().then(
      (s) => {
        if (live) setStatus(s)
      },
      () => {
        if (live) setLoadFailed(true)
      },
    )
    return () => {
      live = false
    }
  }, [])

  async function generate() {
    setError("")
    setMade(null)
    setBusy(true)
    try {
      const result = await makeSelfSigned(
        {
          names: names
            .split(/[,\s]+/)
            .map((n) => n.trim())
            .filter((n) => n !== ""),
          days: Number(days.trim()),
          replace: status?.enabled === true,
        },
        password,
      )
      setMade(result)
      setStatus(await getTls())
    } catch (e) {
      setError(tlsMessage(e))
    } finally {
      setBusy(false)
      setPassword("")
    }
  }

  async function restart() {
    setError("")
    try {
      await restartService()
      setRestarted(true)
    } catch {
      setError(t("tlsCard.didNotWork"))
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("tlsCard.title")}</CardTitle>
        <CardDescription>{t("tlsCard.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {loadFailed ? <Alert variant="destructive">{t("tlsCard.loadFailed")}</Alert> : null}
        {status && !status.enabled ? <p className="text-sm">{t("tlsCard.off")}</p> : null}
        {status?.enabled ? (
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
            <dt className="text-muted-foreground">{t("tlsCard.subject")}</dt>
            <dd>
              {status.subject}{" "}
              <span className="text-muted-foreground">
                ({status.selfSigned ? t("tlsCard.selfSigned") : t("tlsCard.fromAFile")})
              </span>
            </dd>
            <dt className="text-muted-foreground">{t("tlsCard.names")}</dt>
            <dd>{status.names.join(", ")}</dd>
            <dt className="text-muted-foreground">{t("tlsCard.valid")}</dt>
            <dd>
              {t("tlsCard.validRange", {
                from: formatDateTime(status.notBefore),
                to: formatDateTime(status.notAfter),
              })}
            </dd>
            <dt className="text-muted-foreground">{t("tlsCard.fingerprint")}</dt>
            <dd>
              <code className="font-mono text-xs break-all">{status.fingerprint}</code>
            </dd>
          </dl>
        ) : null}

        <div className="flex flex-col gap-2">
          <Label htmlFor="tls-names">{t("tlsCard.names")}</Label>
          <Input id="tls-names" value={names} onChange={(e) => setNames(e.target.value)} />
          <p className="text-muted-foreground text-xs">{t("tlsCard.namesHint")}</p>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="tls-days">{t("tlsCard.days")}</Label>
          <Input
            id="tls-days"
            type="number"
            min={1}
            max={825}
            value={days}
            onChange={(e) => setDays(e.target.value)}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="tls-password">{t("tlsCard.yourPassword")}</Label>
          <Input
            id="tls-password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Button disabled={busy || password === "" || !status} onClick={() => void generate()}>
            {status?.enabled ? t("tlsCard.replace") : t("tlsCard.generate")}
          </Button>
        </div>

        {error ? <Alert variant="destructive">{error}</Alert> : null}
        {made ? (
          <Alert>
            <p>{t("tlsCard.made", { fingerprint: made.fingerprint })}</p>
            {made.needRestart && !restarted ? (
              <p className="mt-2">
                {t("tlsCard.restartToServe")}{" "}
                {canRestart ? (
                  <Button variant="outline" size="sm" onClick={() => void restart()}>
                    {t("settings.restartNow")}
                  </Button>
                ) : (
                  tRich("settings.runCommand", {
                    cmd: (
                      <code className="font-mono">
                        {t("settings.sudoSystemctlRestartNetscribe")}
                      </code>
                    ),
                  })
                )}
              </p>
            ) : null}
          </Alert>
        ) : null}
        <p className="text-muted-foreground text-xs">{t("tlsCard.browserWarns")}</p>
      </CardContent>
    </Card>
  )
}

import { useEffect, useState } from "react"
import {
  ApiError,
  disableTotp,
  enableTotp,
  getTotpStatus,
  setupTotp,
  type TotpSetup,
  type TotpStatus,
} from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { t } from "@/i18n"
import { tRich } from "@/i18n/rich"

function describe(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 403) return t("totpCard.wrongCode")
    if (error.status === 400) return error.message
    if (error.status === 409) return error.message
  }
  return t("totpCard.didNotWork")
}

/** The second factor of the account: an authenticator app and recovery codes. */
export function TotpCard() {
  const [status, setStatus] = useState<TotpStatus | null>(null)
  const [version, setVersion] = useState(0)
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [setup, setSetup] = useState<TotpSetup | null>(null)
  const [recovery, setRecovery] = useState<string[] | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let live = true
    getTotpStatus()
      .then((s) => live && setStatus(s))
      .catch(() => live && setError(t("totpCard.couldNotLoad")))
    return () => {
      live = false
    }
  }, [version])

  async function run(action: () => Promise<void>) {
    setBusy(true)
    setError("")
    try {
      await action()
    } catch (e) {
      setError(describe(e))
    } finally {
      setBusy(false)
      setPassword("")
      setCode("")
    }
  }

  const start = () =>
    run(async () => {
      setSetup(await setupTotp(password))
    })
  const enable = () =>
    run(async () => {
      const done = await enableTotp(code)
      setRecovery(done.recoveryCodes)
      setSetup(null)
      setVersion((v) => v + 1)
    })
  const disable = () =>
    run(async () => {
      await disableTotp(password, code)
      setRecovery(null)
      setVersion((v) => v + 1)
    })

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("totpCard.secondFactor")}</CardTitle>
        <CardDescription>{t("totpCard.aCodeFromAnAuthenticator")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {error ? <Alert variant="destructive">{error}</Alert> : null}
        {recovery ? (
          <Alert>
            <p className="font-medium">{t("totpCard.saveTheseRecoveryCodesNow")}</p>
            <p className="mt-1 text-sm">{t("totpCard.eachOneWorksOneTime")}</p>
            <ul className="mt-2 grid grid-cols-2 gap-1 font-mono text-sm">
              {recovery.map((c) => (
                <li key={c}>{c}</li>
              ))}
            </ul>
          </Alert>
        ) : null}

        {status === null ? null : status.enabled ? (
          <>
            <p className="text-sm">{t("totpCard.on", { n: status.recoveryCodesLeft })}</p>
            <div className="flex flex-col gap-2">
              <Label htmlFor="totp-off-password">{t("totpCard.yourPassword")}</Label>
              <Input
                id="totp-off-password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
              <Label htmlFor="totp-off-code">{t("totpCard.aCurrentCodeOrA")}</Label>
              <Input id="totp-off-code" value={code} onChange={(e) => setCode(e.target.value)} />
            </div>
            <div>
              <Button
                variant="outline"
                disabled={busy || password === "" || code === ""}
                onClick={() => void disable()}
              >
                {t("totpCard.turnOffTheSecondFactor")}
              </Button>
            </div>
          </>
        ) : setup ? (
          <>
            <p className="text-sm">{t("totpCard.addThisKeyToYour")}</p>
            <p className="font-mono text-sm break-all">{setup.secret}</p>
            <p className="text-sm text-muted-foreground">
              {tRich("totpCard.openOnPhone", {
                link: (
                  <a className="underline" href={setup.uri}>
                    {t("totpCard.addToTheApp")}
                  </a>
                ),
              })}
            </p>
            <div className="flex flex-col gap-2">
              <Label htmlFor="totp-code">{t("totpCard.codeFromTheApp")}</Label>
              <Input
                id="totp-code"
                inputMode="numeric"
                autoComplete="one-time-code"
                value={code}
                onChange={(e) => setCode(e.target.value)}
              />
            </div>
            <div>
              <Button disabled={busy || code === ""} onClick={() => void enable()}>
                {t("totpCard.turnOnTheSecondFactor")}
              </Button>
            </div>
          </>
        ) : (
          <>
            <div className="flex flex-col gap-2">
              <Label htmlFor="totp-password">{t("totpCard.yourPassword")}</Label>
              <Input
                id="totp-password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </div>
            <div>
              <Button disabled={busy || password === ""} onClick={() => void start()}>
                {t("totpCard.setUpASecondFactor")}
              </Button>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  )
}

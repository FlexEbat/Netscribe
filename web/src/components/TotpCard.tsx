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

function describe(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 403) return "The password or the code is not right."
    if (error.status === 400) return error.message
    if (error.status === 409) return error.message
  }
  return "That did not work. Try again."
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
      .catch(() => live && setError("The state of the second factor could not be loaded."))
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
        <CardTitle>Second factor</CardTitle>
        <CardDescription>
          A code from an authenticator app, asked for after the password. A stolen password is then
          not enough to sign in.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {error ? <Alert variant="destructive">{error}</Alert> : null}
        {recovery ? (
          <Alert>
            <p className="font-medium">Save these recovery codes now. They are shown once.</p>
            <p className="mt-1 text-sm">Each one works one time, if you lose your phone.</p>
            <ul className="mt-2 grid grid-cols-2 gap-1 font-mono text-sm">
              {recovery.map((c) => (
                <li key={c}>{c}</li>
              ))}
            </ul>
          </Alert>
        ) : null}

        {status === null ? null : status.enabled ? (
          <>
            <p className="text-sm">
              The second factor is on. {status.recoveryCodesLeft} recovery{" "}
              {status.recoveryCodesLeft === 1 ? "code is" : "codes are"} left.
            </p>
            <div className="flex flex-col gap-2">
              <Label htmlFor="totp-off-password">Your password</Label>
              <Input
                id="totp-off-password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
              <Label htmlFor="totp-off-code">A current code or a recovery code</Label>
              <Input id="totp-off-code" value={code} onChange={(e) => setCode(e.target.value)} />
            </div>
            <div>
              <Button
                variant="outline"
                disabled={busy || password === "" || code === ""}
                onClick={() => void disable()}
              >
                Turn off the second factor
              </Button>
            </div>
          </>
        ) : setup ? (
          <>
            <p className="text-sm">
              Add this key to your authenticator app, then type the code it shows.
            </p>
            <p className="font-mono text-sm break-all">{setup.secret}</p>
            <p className="text-sm text-muted-foreground">
              Or open this address on the phone:{" "}
              <a className="underline" href={setup.uri}>
                add to the app
              </a>
            </p>
            <div className="flex flex-col gap-2">
              <Label htmlFor="totp-code">Code from the app</Label>
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
                Turn on the second factor
              </Button>
            </div>
          </>
        ) : (
          <>
            <div className="flex flex-col gap-2">
              <Label htmlFor="totp-password">Your password</Label>
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
                Set up a second factor
              </Button>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  )
}

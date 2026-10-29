import { useCallback, useEffect, useState } from "react"
import { ApiError, createToken, listTokens, revokeToken, type AccessToken } from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import type { Role } from "@/lib/permissions"

interface TokensCardProps {
  /** A viewer cannot issue a token that writes. */
  role: Role
}

type State =
  { status: "loading" } | { status: "failed" } | { status: "ready"; tokens: AccessToken[] }

function statusOf(t: AccessToken): string {
  if (t.revoked) return "Revoked"
  if (new Date(t.expiresAt).getTime() <= Date.now()) return "Expired"
  return "Active"
}

export function TokensCard({ role }: TokensCardProps) {
  const [state, setState] = useState<State>({ status: "loading" })
  const [name, setName] = useState("")
  const [scope, setScope] = useState<"read" | "write">("read")
  const [days, setDays] = useState("90")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [shown, setShown] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [copyFailed, setCopyFailed] = useState(false)

  const load = useCallback(() => {
    listTokens()
      .then((tokens) => setState({ status: "ready", tokens }))
      .catch(() => setState((s) => (s.status === "ready" ? s : { status: "failed" })))
  }, [])

  useEffect(() => {
    load()
  }, [load])

  async function issue(event: React.SyntheticEvent) {
    event.preventDefault()
    setError("")
    setBusy(true)
    try {
      const { value } = await createToken({ name: name.trim(), scope, days: Number(days) })
      setShown(value)
      setCopied(false)
      setCopyFailed(false)
      setName("")
      load()
    } catch (e) {
      if (e instanceof ApiError && e.status === 409)
        setError("You already have 20 active tokens. Revoke one first.")
      else if (e instanceof ApiError && e.status === 400) setError(e.message)
      else setError("The token could not be created. Try again later.")
    } finally {
      setBusy(false)
    }
  }

  async function revoke(token: AccessToken) {
    setError("")
    try {
      await revokeToken(token.id)
      load()
    } catch {
      setError("The token could not be revoked. Try again later.")
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Access tokens</CardTitle>
        <CardDescription>
          For scripts: send a token in the Authorization header as <code>Bearer</code>. A token
          never opens user management or the audit log.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {error ? <Alert variant="destructive">{error}</Alert> : null}
        <form onSubmit={(e) => void issue(e)} className="flex flex-wrap items-end gap-3" noValidate>
          <div className="flex flex-col gap-2">
            <Label htmlFor="token-name">Token name</Label>
            <Input
              id="token-name"
              value={name}
              maxLength={64}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="token-scope">Scope</Label>
            <select
              id="token-scope"
              className="h-9 rounded-lg border bg-input px-3 text-sm"
              value={scope}
              onChange={(e) => setScope(e.target.value as "read" | "write")}
            >
              <option value="read">Read</option>
              {role !== "viewer" ? <option value="write">Write</option> : null}
            </select>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="token-days">Valid for (days)</Label>
            <Input
              id="token-days"
              type="number"
              min={1}
              max={365}
              className="w-28"
              value={days}
              onChange={(e) => setDays(e.target.value)}
            />
          </div>
          <Button type="submit" disabled={busy}>
            {busy ? "Creating…" : "Create token"}
          </Button>
        </form>

        {state.status === "loading" ? (
          <div role="status" aria-label="Loading tokens" className="flex flex-col gap-2">
            <Skeleton className="h-10 w-full" />
            <Skeleton className="h-10 w-full" />
          </div>
        ) : state.status === "failed" ? (
          <Alert variant="destructive">The tokens could not be loaded.</Alert>
        ) : state.tokens.length === 0 ? (
          <p className="text-sm text-muted-foreground">You have no access tokens.</p>
        ) : (
          <ul className="flex flex-col divide-y rounded-lg border text-sm">
            {state.tokens.map((t) => (
              <li key={t.id} className="flex flex-wrap items-center gap-3 px-3 py-2">
                <span className="font-medium">{t.name}</span>
                <code className="text-xs">{t.prefix}…</code>
                <span>{t.scope}</span>
                <span className="text-muted-foreground">
                  {statusOf(t)}, until {new Date(t.expiresAt).toLocaleDateString()}
                </span>
                {statusOf(t) === "Active" ? (
                  <Button
                    variant="outline"
                    size="sm"
                    className="ml-auto"
                    aria-label={`Revoke ${t.name}`}
                    onClick={() => void revoke(t)}
                  >
                    Revoke
                  </Button>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </CardContent>

      {shown ? (
        <Dialog open onOpenChange={(open) => !open && setShown(null)}>
          <DialogContent aria-describedby={undefined}>
            <DialogTitle className="text-lg font-semibold">Your new token</DialogTitle>
            <DialogDescription className="text-sm text-muted-foreground">
              Copy it now. It is shown only once and cannot be recovered.
            </DialogDescription>
            <code className="rounded-md border bg-muted px-3 py-2 text-sm break-all select-all">
              {shown}
            </code>
            {copyFailed ? (
              <Alert variant="destructive">
                The browser did not allow copying. Select the token above and copy it by hand.
              </Alert>
            ) : null}
            <div className="flex justify-end gap-2">
              <Button
                variant="outline"
                onClick={() => {
                  // navigator.clipboard exists only on HTTPS and localhost, and a LAN
                  // panel is often served over plain HTTP. The token is shown once, so a
                  // silent failure here would cost the user the token.
                  const write = navigator.clipboard?.writeText(shown)
                  if (!write) {
                    setCopyFailed(true)
                    return
                  }
                  write.then(
                    () => {
                      setCopied(true)
                      setCopyFailed(false)
                    },
                    () => setCopyFailed(true),
                  )
                }}
              >
                {copied ? "Copied" : "Copy"}
              </Button>
              <Button onClick={() => setShown(null)}>Done</Button>
            </div>
          </DialogContent>
        </Dialog>
      ) : null}
    </Card>
  )
}

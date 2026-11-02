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
import { formatDate, t } from "@/i18n"
import { tRich } from "@/i18n/rich"

interface TokensCardProps {
  /** A viewer cannot issue a token that writes. */
  role: Role
}

type State =
  { status: "loading" } | { status: "failed" } | { status: "ready"; tokens: AccessToken[] }

const statusLabels = {
  revoked: "tokensCard.revoked",
  expired: "tokensCard.expired",
  active: "tokensCard.active",
} as const

function statusOf(tok: AccessToken): "revoked" | "expired" | "active" {
  if (tok.revoked) return "revoked"
  if (new Date(tok.expiresAt).getTime() <= Date.now()) return "expired"
  return "active"
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
      if (e instanceof ApiError && e.status === 409) setError(t("tokensCard.tooMany"))
      else if (e instanceof ApiError && e.status === 400) setError(e.message)
      else setError(t("tokensCard.couldNotCreate"))
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
      setError(t("tokensCard.couldNotRevoke"))
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("tokensCard.accessTokens")}</CardTitle>
        <CardDescription>
          {tRich("tokensCard.description", {
            bearer: <code>{t("tokensCard.bearer")}</code>,
          })}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {error ? <Alert variant="destructive">{error}</Alert> : null}
        <form onSubmit={(e) => void issue(e)} className="flex flex-wrap items-end gap-3" noValidate>
          <div className="flex flex-col gap-2">
            <Label htmlFor="token-name">{t("tokensCard.tokenName")}</Label>
            <Input
              id="token-name"
              value={name}
              maxLength={64}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="token-scope">{t("tokensCard.scope")}</Label>
            <select
              id="token-scope"
              className="h-9 rounded-lg border bg-input px-3 text-sm"
              value={scope}
              onChange={(e) => setScope(e.target.value as "read" | "write")}
            >
              <option value="read">{t("tokensCard.read")}</option>
              {role !== "viewer" ? <option value="write">{t("tokensCard.write")}</option> : null}
            </select>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="token-days">{t("tokensCard.validForDays")}</Label>
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
            {t(busy ? "tokensCard.creating" : "tokensCard.createToken")}
          </Button>
        </form>

        {state.status === "loading" ? (
          <div
            role="status"
            aria-label={t("tokensCard.loadingTokens")}
            className="flex flex-col gap-2"
          >
            <Skeleton className="h-10 w-full" />
            <Skeleton className="h-10 w-full" />
          </div>
        ) : state.status === "failed" ? (
          <Alert variant="destructive">{t("tokensCard.theTokensCouldNotBe")}</Alert>
        ) : state.tokens.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("tokensCard.youHaveNoAccessTokens")}</p>
        ) : (
          <ul className="flex flex-col divide-y rounded-lg border text-sm">
            {state.tokens.map((tok) => (
              <li key={tok.id} className="flex flex-wrap items-center gap-3 px-3 py-2">
                <span className="font-medium">{tok.name}</span>
                <code className="text-xs">{tok.prefix}…</code>
                <span>{tok.scope}</span>
                <span className="text-muted-foreground">
                  {t("tokensCard.statusUntil", {
                    status: t(statusLabels[statusOf(tok)]),
                    date: formatDate(tok.expiresAt),
                  })}
                </span>
                {statusOf(tok) === "active" ? (
                  <Button
                    variant="outline"
                    size="sm"
                    className="ml-auto"
                    aria-label={t("tokensCard.revokeName", { name: tok.name })}
                    onClick={() => void revoke(tok)}
                  >
                    {t("tokensCard.revoke")}
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
            <DialogTitle className="text-lg font-semibold">
              {t("tokensCard.yourNewToken")}
            </DialogTitle>
            <DialogDescription className="text-sm text-muted-foreground">
              {t("tokensCard.copyItNowItIs")}
            </DialogDescription>
            <code className="rounded-md border bg-muted px-3 py-2 text-sm break-all select-all">
              {shown}
            </code>
            {copyFailed ? (
              <Alert variant="destructive">{t("tokensCard.theBrowserDidNotAllow")}</Alert>
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
                {t(copied ? "tokensCard.copied" : "tokensCard.copy")}
              </Button>
              <Button onClick={() => setShown(null)}>{t("tokensCard.done")}</Button>
            </div>
          </DialogContent>
        </Dialog>
      ) : null}
    </Card>
  )
}

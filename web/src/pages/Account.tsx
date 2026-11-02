import { useState, type FormEvent } from "react"
import { ApiError, api } from "@/api/client"
import { useSession } from "@/api/session"
import { can } from "@/lib/permissions"
import { RoleBadge } from "@/components/RoleBadge"
import { SessionsCard } from "@/components/SessionsCard"
import { TotpCard } from "@/components/TotpCard"
import { TokensCard } from "@/components/TokensCard"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { t } from "@/i18n"

export function Account() {
  const { state, forget } = useSession()
  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [repeat, setRepeat] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  if (state.status !== "signedIn") return null
  const { user } = state.session

  async function onSubmit(event: FormEvent) {
    event.preventDefault()
    setError(null)
    if (next !== repeat) {
      setError(t("account.noMatch"))
      return
    }
    setBusy(true)
    try {
      await api("PUT", "/api/auth/password", {
        body: { currentPassword: current, newPassword: next },
      })
      // The server ended every session of the user, this one included.
      forget(t("account.passwordChanged"))
    } catch (e) {
      setError(
        e instanceof ApiError && e.status === 400 ? e.message : t("account.passwordNotChanged"),
      )
      setBusy(false)
    }
  }

  return (
    <div className="flex max-w-xl flex-col gap-6">
      <h1 className="text-2xl font-semibold">{t("account.account")}</h1>

      <Card>
        <CardHeader>
          <CardTitle>{user.displayName || user.username}</CardTitle>
          <CardDescription>{t("account.signedInAs", { name: user.username })}</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-2 text-sm">
            {t("account.role")} <RoleBadge role={user.role} />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("account.changePassword")}</CardTitle>
          <CardDescription>{t("account.youWillBeSignedOut")}</CardDescription>
        </CardHeader>
        <CardContent>
          {user.mustChangePassword ? <Alert>{t("account.youMustSetANew")}</Alert> : null}
          {error ? <Alert variant="destructive">{error}</Alert> : null}
          <form onSubmit={onSubmit} className="flex flex-col gap-4">
            <div className="flex flex-col gap-2">
              <Label htmlFor="current-password">{t("account.currentPassword")}</Label>
              <Input
                id="current-password"
                type="password"
                value={current}
                onChange={(e) => setCurrent(e.target.value)}
                autoComplete="current-password"
                required
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="new-password">{t("account.newPassword")}</Label>
              <Input
                id="new-password"
                type="password"
                value={next}
                onChange={(e) => setNext(e.target.value)}
                autoComplete="new-password"
                aria-describedby="new-password-hint"
                required
              />
              <p id="new-password-hint" className="text-sm text-muted-foreground">
                {t("account.aLongPhraseWorksBetter")}
              </p>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="repeat-password">{t("account.repeatTheNewPassword")}</Label>
              <Input
                id="repeat-password"
                type="password"
                value={repeat}
                onChange={(e) => setRepeat(e.target.value)}
                autoComplete="new-password"
                required
              />
            </div>
            <Button type="submit" disabled={busy}>
              {t(busy ? "account.saving" : "account.changePassword")}
            </Button>
          </form>
        </CardContent>
      </Card>

      <TotpCard />

      <SessionsCard />

      {can(state.session.permissions, "tokens:manage") ? <TokensCard role={user.role} /> : null}
    </div>
  )
}

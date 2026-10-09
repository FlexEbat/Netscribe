import { useState, type FormEvent } from "react"
import { ApiError, api } from "@/api/client"
import { useSession } from "@/api/session"
import { RoleBadge } from "@/components/RoleBadge"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

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
      setError("The new passwords do not match.")
      return
    }
    setBusy(true)
    try {
      await api("PUT", "/api/auth/password", {
        body: { currentPassword: current, newPassword: next },
      })
      // The server ended every session of the user, this one included.
      forget("Your password was changed. Sign in with the new one.")
    } catch (e) {
      setError(
        e instanceof ApiError && e.status === 400
          ? e.message
          : "The password was not changed. Try again later.",
      )
      setBusy(false)
    }
  }

  return (
    <div className="flex max-w-xl flex-col gap-6">
      <h1 className="text-2xl font-semibold">Account</h1>

      <Card>
        <CardHeader>
          <CardTitle>{user.displayName || user.username}</CardTitle>
          <CardDescription>Signed in as {user.username}</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-2 text-sm">
            Role <RoleBadge role={user.role} />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Change password</CardTitle>
          <CardDescription>
            You will be signed out everywhere and asked to sign in with the new password.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {user.mustChangePassword ? (
            <Alert>You must set a new password before you can continue.</Alert>
          ) : null}
          {error ? <Alert variant="destructive">{error}</Alert> : null}
          <form onSubmit={onSubmit} className="flex flex-col gap-4">
            <div className="flex flex-col gap-2">
              <Label htmlFor="current-password">Current password</Label>
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
              <Label htmlFor="new-password">New password</Label>
              <Input
                id="new-password"
                type="password"
                value={next}
                onChange={(e) => setNext(e.target.value)}
                autoComplete="new-password"
                required
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="repeat-password">Repeat the new password</Label>
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
              {busy ? "Saving…" : "Change password"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}

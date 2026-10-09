import { useEffect, useRef, useState, type FormEvent } from "react"
import { Navigate, useLocation } from "react-router-dom"
import { ApiError } from "@/api/client"
import { useSession } from "@/api/session"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

function describe(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 401) return "Invalid username or password."
    if (error.status === 429) {
      const wait = error.retryAfter ? ` in ${error.retryAfter} seconds` : " later"
      return `Too many attempts. Try again${wait}.`
    }
    if (error.status === 400) return "Enter a username and a password."
    if (error.status === 503) return "The server is not ready yet."
  }
  return "Sign-in failed. Try again later."
}

export function Login() {
  const { state, signIn } = useSession()
  const location = useLocation()
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const usernameRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    usernameRef.current?.focus()
  }, [])

  if (state.status === "signedIn") {
    const from = (location.state as { from?: string } | null)?.from
    return <Navigate to={from && from !== "/login" ? from : "/"} replace />
  }
  const notice = state.status === "anonymous" ? state.notice : undefined

  async function onSubmit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await signIn(username, password)
    } catch (e) {
      setError(describe(e))
      setPassword("")
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-muted p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Netscribe</CardTitle>
          <CardDescription>Sign in to continue.</CardDescription>
        </CardHeader>
        <CardContent>
          {notice ? <Alert>{notice}</Alert> : null}
          {error ? <Alert variant="destructive">{error}</Alert> : null}
          <form onSubmit={onSubmit} className="flex flex-col gap-4">
            <div className="flex flex-col gap-2">
              <Label htmlFor="username">Username</Label>
              <Input
                id="username"
                ref={usernameRef}
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                autoComplete="username"
                autoCapitalize="none"
                spellCheck={false}
                required
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="password">Password</Label>
              <Input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
                required
              />
            </div>
            <Button type="submit" disabled={busy}>
              {busy ? "Signing in…" : "Sign in"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </main>
  )
}

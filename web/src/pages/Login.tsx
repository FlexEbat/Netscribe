import { useEffect, useRef, useState, type FormEvent } from "react"
import { Navigate, useLocation } from "react-router-dom"
import { ApiError, getProviders } from "@/api/client"
import { useSession } from "@/api/session"
import { BrandMark } from "@/components/Brand"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { t } from "@/i18n"

function describe(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 401) return t("login.invalid")
    if (error.status === 429) {
      return error.retryAfter
        ? t("login.tooManyIn", { n: error.retryAfter })
        : t("login.tooManyLater")
    }
    if (error.status === 400) return t("login.enterBoth")
    if (error.status === 503) return t("login.notReady")
  }
  return t("login.failed")
}

export function Login() {
  const { state, signIn, signInWithCode } = useSession()
  const location = useLocation()
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [challenge, setChallenge] = useState<string | null>(null)
  const [code, setCode] = useState("")
  const usernameRef = useRef<HTMLInputElement>(null)
  const codeRef = useRef<HTMLInputElement>(null)
  const [ssoName, setSsoName] = useState<string | null>(null)
  const ssoFailed = new URLSearchParams(location.search).get("sso") === "failed"

  useEffect(() => {
    usernameRef.current?.focus()
  }, [])

  useEffect(() => {
    // A server that has no single sign-on, or one that does not answer, shows no button.
    let alive = true
    getProviders()
      .then((p) => {
        if (alive && p.oidc) setSsoName(p.oidc.name)
      })
      .catch(() => {})
    return () => {
      alive = false
    }
  }, [])

  useEffect(() => {
    if (challenge) codeRef.current?.focus()
  }, [challenge])

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
      if (challenge) {
        await signInWithCode(challenge, code)
      } else {
        setChallenge(await signIn(username, password))
      }
    } catch (e) {
      if (challenge) {
        // the challenge lives for five minutes and five tries: after a refusal start again
        setError(t("login.codeWrong"))
        setChallenge(null)
        setCode("")
      } else {
        setError(describe(e))
      }
      setPassword("")
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="grid min-h-screen place-items-center p-4">
      <div className="page-enter w-full max-w-sm">
        <div className="flex flex-col gap-6">
          <div className="flex flex-col items-center gap-3">
            <BrandMark className="size-12 rounded-2xl" />
            <h1 className="hero-heading text-center text-3xl font-semibold">
              {t("login.networkDocumentationKeptCurrent")}
            </h1>
          </div>
          <Card className="gap-6 p-8">
            <CardHeader>
              <CardTitle className="text-xl">{t("login.netscribe")}</CardTitle>
              <CardDescription>{t("login.signInToContinue")}</CardDescription>
            </CardHeader>
            <CardContent>
              {notice ? <Alert>{notice}</Alert> : null}
              {error ? <Alert variant="destructive">{error}</Alert> : null}
              {ssoFailed && !error ? (
                <Alert variant="destructive">{t("login.ssoFailed")}</Alert>
              ) : null}
              <form onSubmit={onSubmit} className="flex flex-col gap-4">
                {challenge ? (
                  <div className="flex flex-col gap-2">
                    <Label htmlFor="code">{t("login.authenticationCode")}</Label>
                    <Input
                      id="code"
                      value={code}
                      onChange={(e) => setCode(e.target.value)}
                      autoComplete="one-time-code"
                      inputMode="numeric"
                      ref={codeRef}
                      required
                    />
                    <p className="text-sm text-muted-foreground">
                      {t("login.theCodeFromYourAuthenticator")}
                    </p>
                  </div>
                ) : null}
                <div className={challenge ? "hidden" : "flex flex-col gap-2"}>
                  <Label htmlFor="username">{t("login.username")}</Label>
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
                <div className={challenge ? "hidden" : "flex flex-col gap-2"}>
                  <Label htmlFor="password">{t("login.password")}</Label>
                  <Input
                    id="password"
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    autoComplete="current-password"
                    required
                  />
                </div>
                <Button type="submit" disabled={busy} className="mt-2 h-10">
                  {t(busy ? "login.signingIn" : challenge ? "login.verify" : "login.signIn")}
                </Button>
              </form>
              {ssoName && !challenge ? (
                <Button asChild variant="outline" className="h-10">
                  {/* A plain link: the provider needs a full page navigation, not a fetch. */}
                  <a href="/api/auth/oidc/login">{t("login.ssoSignIn", { name: ssoName })}</a>
                </Button>
              ) : null}
            </CardContent>
          </Card>
        </div>
      </div>
    </main>
  )
}

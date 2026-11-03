import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react"
import type { Permission, User } from "@/lib/permissions"
import { ApiError, api, setCsrfToken, setUnauthorizedHandler } from "./client"
import { t } from "@/i18n"

interface MeResponse {
  user: User
  permissions: Permission[]
  csrfToken: string
}

export interface Session {
  user: User
  permissions: Permission[]
}

type State =
  | { status: "loading" }
  | { status: "anonymous"; notice?: string }
  | { status: "signedIn"; session: Session }

interface SessionApi {
  state: State
  /** Resolves to a challenge when the account also needs a code, and to null when it is in. */
  signIn: (username: string, password: string) => Promise<string | null>
  /** The second step of a sign in, for an account with a second factor. */
  signInWithCode: (challenge: string, code: string) => Promise<void>
  signOut: () => Promise<void>
  /** Forget the session without asking the server, for when it is already gone. */
  forget: (notice?: string) => void
}

const SessionContext = createContext<SessionApi | null>(null)

function toSession(me: MeResponse): Session {
  setCsrfToken(me.csrfToken)
  return { user: me.user, permissions: me.permissions }
}

export function SessionProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<State>({ status: "loading" })

  const forget = useCallback((notice?: string) => {
    setCsrfToken("")
    setState({ status: "anonymous", notice })
  }, [])

  useEffect(() => {
    setUnauthorizedHandler(() => forget())
    return () => setUnauthorizedHandler(null)
  }, [forget])

  useEffect(() => {
    let cancelled = false
    api<MeResponse>("GET", "/api/auth/me", { expectUnauthorized: true })
      .then((me) => {
        if (!cancelled) setState({ status: "signedIn", session: toSession(me) })
      })
      .catch((err: unknown) => {
        if (cancelled) return
        // 401 only means nobody is signed in. Any other failure (no network, a proxy
        // error, the server still starting) must not look like a signed-out visitor,
        // or the sign-in form that follows fails with no hint why.
        const signedOut = err instanceof ApiError && err.status === 401
        setState({
          status: "anonymous",
          notice: signedOut ? undefined : t("session.noAnswer"),
        })
      })
    return () => {
      cancelled = true
    }
  }, [])

  const signIn = useCallback(async (username: string, password: string) => {
    const answer = await api<MeResponse | { mfa: true; challenge: string }>(
      "POST",
      "/api/auth/login",
      {
        body: { username, password },
        expectUnauthorized: true,
      },
    )
    if ("mfa" in answer) return answer.challenge
    setState({ status: "signedIn", session: toSession(answer) })
    return null
  }, [])

  const signInWithCode = useCallback(async (challenge: string, code: string) => {
    const me = await api<MeResponse>("POST", "/api/auth/login/totp", {
      body: { challenge, code },
      expectUnauthorized: true,
    })
    setState({ status: "signedIn", session: toSession(me) })
  }, [])

  const signOut = useCallback(async () => {
    try {
      await api("POST", "/api/auth/logout")
    } catch (error) {
      // A session that is already gone is the outcome we wanted.
      if (!(error instanceof ApiError) || error.status !== 401) throw error
    } finally {
      forget()
    }
  }, [forget])

  const value = useMemo(
    () => ({ state, signIn, signInWithCode, signOut, forget }),
    [state, signIn, signInWithCode, signOut, forget],
  )
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
}

export function useSession(): SessionApi {
  const value = useContext(SessionContext)
  if (!value) throw new Error("useSession must be used inside SessionProvider")
  return value
}

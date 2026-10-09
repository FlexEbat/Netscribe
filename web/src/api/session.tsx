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
  signIn: (username: string, password: string) => Promise<void>
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
      .catch(() => {
        if (!cancelled) setState({ status: "anonymous" })
      })
    return () => {
      cancelled = true
    }
  }, [])

  const signIn = useCallback(async (username: string, password: string) => {
    const me = await api<MeResponse>("POST", "/api/auth/login", {
      body: { username, password },
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
    () => ({ state, signIn, signOut, forget }),
    [state, signIn, signOut, forget],
  )
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
}

export function useSession(): SessionApi {
  const value = useContext(SessionContext)
  if (!value) throw new Error("useSession must be used inside SessionProvider")
  return value
}

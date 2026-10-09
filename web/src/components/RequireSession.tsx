import type { ReactNode } from "react"
import { Navigate, useLocation } from "react-router-dom"
import { useSession } from "@/api/session"

/**
 * Keeps signed-out visitors on the login page, and keeps a user who must change the
 * password on the account page until they have.
 */
export function RequireSession({ children }: { children: ReactNode }) {
  const { state } = useSession()
  const location = useLocation()

  if (state.status === "loading") {
    return (
      <p role="status" className="p-6 text-sm text-muted-foreground">
        Loading…
      </p>
    )
  }
  if (state.status === "anonymous") {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }
  if (state.session.user.mustChangePassword && location.pathname !== "/account") {
    return <Navigate to="/account" replace />
  }
  return <>{children}</>
}

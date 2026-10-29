import type { ReactNode } from "react"
import { Navigate, useLocation } from "react-router-dom"
import { Skeleton } from "@/components/ui/skeleton"
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
      <div role="status" aria-label="Loading the session" className="flex min-h-screen gap-6 p-6">
        <Skeleton className="hidden h-full w-64 md:block" />
        <div className="flex flex-1 flex-col gap-4">
          <Skeleton className="h-8 w-48" />
          <Skeleton className="h-48 w-full" />
        </div>
      </div>
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

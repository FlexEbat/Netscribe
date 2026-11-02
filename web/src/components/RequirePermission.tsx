import type { ReactNode } from "react"
import { useSession } from "@/api/session"
import { EmptyState } from "@/components/EmptyState"
import { can, type Permission } from "@/lib/permissions"
import { t } from "@/i18n"

interface RequirePermissionProps {
  permission: Permission
  children: ReactNode
}

/** Shows the children only to a user who holds the permission. The server enforces it too. */
export function RequirePermission({ permission, children }: RequirePermissionProps) {
  const { state } = useSession()
  if (state.status !== "signedIn") return null
  if (!can(state.session.permissions, permission)) {
    return (
      <EmptyState
        title={t("requirePermission.noAccess")}
        text={t("requirePermission.noAccessText")}
      />
    )
  }
  return <>{children}</>
}

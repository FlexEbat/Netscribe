import { t } from "@/i18n"
export type Role = "viewer" | "operator" | "admin"

export type Permission =
  | "topology:read"
  | "scans:run"
  | "devices:write"
  | "export:run"
  | "tokens:manage"
  | "audit:read"
  | "users:manage"
  | "system:manage"

export interface User {
  id: number
  username: string
  displayName: string
  role: Role
  disabled: boolean
  mustChangePassword: boolean
  createdAt: string
  lastLoginAt: string | null
}

export const roleLabels: Record<Role, string> = {
  get viewer() {
    return t("role.viewer")
  },
  get operator() {
    return t("role.operator")
  },
  get admin() {
    return t("role.admin")
  },
}

/** Hides what the server would refuse anyway. The server stays the only judge. */
export function can(permissions: readonly Permission[], permission: Permission): boolean {
  return permissions.includes(permission)
}

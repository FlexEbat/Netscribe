import type { Key } from "@/i18n"
import type { Permission } from "@/lib/permissions"

export type NavId =
  | "topology"
  | "overview"
  | "groups"
  | "inventory"
  | "services"
  | "containers"
  | "changes"
  | "reference"
  | "users"
  | "audit"
  | "health"
  | "settings"

export interface NavItem {
  id: NavId
  to: string
  label: Key
  /** One line under the page title. */
  subtitle: Key
  permission: Permission
  group: "workspace" | "admin"
}

/** The sheets of the panel, in order: the index, the page title and the breadcrumb read from here. */
export const navItems: NavItem[] = [
  {
    id: "topology",
    to: "/",
    label: "nav.topology",
    subtitle: "pageSubtitle.topology",
    permission: "topology:read",
    group: "workspace",
  },
  {
    id: "overview",
    to: "/overview",
    label: "nav.overview",
    subtitle: "pageSubtitle.overview",
    permission: "topology:read",
    group: "workspace",
  },
  {
    id: "groups",
    to: "/groups",
    label: "nav.groups",
    subtitle: "pageSubtitle.groups",
    permission: "topology:read",
    group: "workspace",
  },
  {
    id: "inventory",
    to: "/inventory",
    label: "nav.inventory",
    subtitle: "pageSubtitle.inventory",
    permission: "topology:read",
    group: "workspace",
  },
  {
    id: "services",
    to: "/services",
    label: "nav.services",
    subtitle: "pageSubtitle.services",
    permission: "topology:read",
    group: "workspace",
  },
  {
    id: "containers",
    to: "/containers",
    label: "nav.containers",
    subtitle: "pageSubtitle.containers",
    permission: "topology:read",
    group: "workspace",
  },
  {
    id: "changes",
    to: "/changes",
    label: "nav.changes",
    subtitle: "pageSubtitle.changes",
    permission: "topology:read",
    group: "workspace",
  },
  {
    id: "reference",
    to: "/reference",
    label: "nav.apiReference",
    subtitle: "pageSubtitle.reference",
    permission: "topology:read",
    group: "workspace",
  },
  {
    id: "users",
    to: "/users",
    label: "nav.users",
    subtitle: "pageSubtitle.users",
    permission: "users:manage",
    group: "admin",
  },
  {
    id: "audit",
    to: "/audit",
    label: "nav.auditLog",
    subtitle: "pageSubtitle.audit",
    permission: "audit:read",
    group: "admin",
  },
  {
    id: "health",
    to: "/health",
    label: "nav.health",
    subtitle: "pageSubtitle.health",
    permission: "system:manage",
    group: "admin",
  },
  {
    id: "settings",
    to: "/settings",
    label: "nav.settings",
    subtitle: "pageSubtitle.settings",
    permission: "system:manage",
    group: "admin",
  },
]

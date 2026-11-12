import {
  BracketsCurly,
  ClockCounterClockwise,
  Cube,
  Gauge,
  Heartbeat,
  ListBullets,
  PlugsConnected,
  Scroll,
  ShareNetwork,
  SlidersHorizontal,
  Stack,
  Users as UsersIcon,
  type Icon,
} from "@phosphor-icons/react"
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
  icon: Icon
  /** The hue of the icon tile. It is the only place a page gets a color of its own. */
  tint: string
  permission: Permission
  group: "workspace" | "admin"
}

/** The pages of the panel: the sidebar, the page title and the route label read from here. */
export const navItems: NavItem[] = [
  {
    id: "topology",
    to: "/",
    label: "nav.topology",
    subtitle: "pageSubtitle.topology",
    icon: ShareNetwork,
    tint: "#7b93ff",
    permission: "topology:read",
    group: "workspace",
  },
  {
    id: "overview",
    to: "/overview",
    label: "nav.overview",
    subtitle: "pageSubtitle.overview",
    icon: Gauge,
    tint: "#38bdf8",
    permission: "topology:read",
    group: "workspace",
  },
  {
    id: "groups",
    to: "/groups",
    label: "nav.groups",
    subtitle: "pageSubtitle.groups",
    icon: Stack,
    tint: "#a78bfa",
    permission: "topology:read",
    group: "workspace",
  },
  {
    id: "inventory",
    to: "/inventory",
    label: "nav.inventory",
    subtitle: "pageSubtitle.inventory",
    icon: ListBullets,
    tint: "#2dd4bf",
    permission: "topology:read",
    group: "workspace",
  },
  {
    id: "services",
    to: "/services",
    label: "nav.services",
    subtitle: "pageSubtitle.services",
    icon: PlugsConnected,
    tint: "#fbbf24",
    permission: "topology:read",
    group: "workspace",
  },
  {
    id: "containers",
    to: "/containers",
    label: "nav.containers",
    subtitle: "pageSubtitle.containers",
    icon: Cube,
    tint: "#34d399",
    permission: "topology:read",
    group: "workspace",
  },
  {
    id: "changes",
    to: "/changes",
    label: "nav.changes",
    subtitle: "pageSubtitle.changes",
    icon: ClockCounterClockwise,
    tint: "#fb923c",
    permission: "topology:read",
    group: "workspace",
  },
  {
    id: "reference",
    to: "/reference",
    label: "nav.apiReference",
    subtitle: "pageSubtitle.reference",
    icon: BracketsCurly,
    tint: "#94a3b8",
    permission: "topology:read",
    group: "workspace",
  },
  {
    id: "users",
    to: "/users",
    label: "nav.users",
    subtitle: "pageSubtitle.users",
    icon: UsersIcon,
    tint: "#f472b6",
    permission: "users:manage",
    group: "admin",
  },
  {
    id: "audit",
    to: "/audit",
    label: "nav.auditLog",
    subtitle: "pageSubtitle.audit",
    icon: Scroll,
    tint: "#facc15",
    permission: "audit:read",
    group: "admin",
  },
  {
    id: "health",
    to: "/health",
    label: "nav.health",
    subtitle: "pageSubtitle.health",
    icon: Heartbeat,
    tint: "#4ade80",
    permission: "system:manage",
    group: "admin",
  },
  {
    id: "settings",
    to: "/settings",
    label: "nav.settings",
    subtitle: "pageSubtitle.settings",
    icon: SlidersHorizontal,
    tint: "#cbd5e1",
    permission: "system:manage",
    group: "admin",
  },
]

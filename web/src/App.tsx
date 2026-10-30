import {
  ClockCounterClockwise,
  Cube,
  Gauge,
  List,
  ListBullets,
  Moon,
  PlugsConnected,
  Scroll,
  ShareNetwork,
  SlidersHorizontal,
  Sun,
  User as UserIcon,
  Users as UsersIcon,
  X,
} from "@phosphor-icons/react"
import { useState, type CSSProperties } from "react"
import { NavLink, Outlet, useLocation, type RouteObject } from "react-router-dom"
import { useSession } from "@/api/session"
import { Brand, BrandMark } from "@/components/Brand"
import { EmptyState } from "@/components/EmptyState"
import { ErrorBoundary } from "@/components/ErrorBoundary"
import { RequirePermission } from "@/components/RequirePermission"
import { RequireSession } from "@/components/RequireSession"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet"
import { UserMenu } from "@/components/UserMenu"
import { VersionBadge } from "@/components/VersionBadge"
import { can, type Permission, type User } from "@/lib/permissions"
import { setTheme, useTheme } from "@/lib/theme"
import { cn } from "@/lib/utils"
import { Account } from "@/pages/Account"
import { Audit } from "@/pages/Audit"
import { Users } from "@/pages/Users"
import { Inventory } from "@/pages/Inventory"
import { Overview } from "@/pages/Overview"
import { Login } from "@/pages/Login"
import { Changes } from "@/pages/Changes"
import { Containers } from "@/pages/Containers"
import { Services } from "@/pages/Services"
import { Settings } from "@/pages/Settings"
import { Topology } from "@/pages/Topology"

// The pages of the panel and the navigation that leads to them.
interface NavItem {
  to: string
  label: string
  icon: typeof ShareNetwork
  permission: Permission
  group: "workspace" | "admin"
}

const navItems: NavItem[] = [
  {
    to: "/",
    label: "Topology",
    icon: ShareNetwork,
    permission: "topology:read",
    group: "workspace",
  },
  {
    to: "/overview",
    label: "Overview",
    icon: Gauge,
    permission: "topology:read",
    group: "workspace",
  },
  {
    to: "/inventory",
    label: "Inventory",
    icon: ListBullets,
    permission: "topology:read",
    group: "workspace",
  },
  {
    to: "/services",
    label: "Services",
    icon: PlugsConnected,
    permission: "topology:read",
    group: "workspace",
  },
  {
    to: "/containers",
    label: "Containers",
    icon: Cube,
    permission: "topology:read",
    group: "workspace",
  },
  {
    to: "/changes",
    label: "Changes",
    icon: ClockCounterClockwise,
    permission: "topology:read",
    group: "workspace",
  },
  { to: "/users", label: "Users", icon: UsersIcon, permission: "users:manage", group: "admin" },
  { to: "/audit", label: "Audit log", icon: Scroll, permission: "audit:read", group: "admin" },
  {
    to: "/settings",
    label: "Settings",
    icon: SlidersHorizontal,
    permission: "system:manage",
    group: "admin",
  },
]

const groupLabels = { workspace: "Workspace", admin: "Administration" } as const

interface SidebarProps {
  user: User
  permissions: Permission[]
  onSignOut: () => void
  onNavigate?: () => void
}

function NavGroup({
  group,
  items,
  offset,
  onNavigate,
}: {
  group: NavItem["group"]
  items: NavItem[]
  /** How many links come before this group, so the entrance runs in reading order. */
  offset: number
  onNavigate?: () => void
}) {
  return (
    <div className="flex flex-col gap-1">
      <p className="px-3 pb-1 text-xs font-medium tracking-wider text-muted-foreground uppercase">
        {groupLabels[group]}
      </p>
      {items.map(({ to, label, icon: Icon }, index) => (
        <NavLink
          key={to}
          to={to}
          end
          onClick={onNavigate}
          style={{ "--i": offset + index } as CSSProperties}
          className={({ isActive }) =>
            cn(
              "nav-item group relative flex items-center gap-3 rounded-xl px-3 py-2 text-sm transition-[background-color,color] focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
              isActive
                ? "bg-accent font-medium text-foreground shadow-[inset_0_1px_0_var(--hairline)]"
                : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
            )
          }
        >
          {({ isActive }) => (
            <>
              <span
                aria-hidden="true"
                className={cn(
                  "absolute top-1/2 -left-4 h-5 w-[3px] -translate-y-1/2 rounded-full bg-brand shadow-[0_0_14px_var(--glow)] transition-[opacity,transform]",
                  isActive ? "scale-y-100 opacity-100" : "scale-y-50 opacity-0",
                )}
              />
              <Icon
                aria-hidden="true"
                className={cn(
                  "size-5 transition-[transform,color] group-hover:-translate-y-px group-hover:scale-110",
                  isActive && "text-brand",
                )}
              />
              {label}
            </>
          )}
        </NavLink>
      ))}
    </div>
  )
}

function Sidebar({ user, permissions, onSignOut, onNavigate }: SidebarProps) {
  const allowed = navItems.filter((item) => can(permissions, item.permission))
  const groups = (["workspace", "admin"] as const).filter((g) => allowed.some((i) => i.group === g))
  return (
    <>
      <div className="flex flex-col gap-8">
        <Brand className="px-2 pt-1" />
        <nav aria-label="Main" className="flex flex-col gap-6">
          {groups.map((g) => (
            <NavGroup
              key={g}
              group={g}
              items={allowed.filter((i) => i.group === g)}
              offset={allowed.findIndex((i) => i.group === g)}
              onNavigate={onNavigate}
            />
          ))}
        </nav>
      </div>
      <UserMenu user={user} onSignOut={onSignOut} />
    </>
  )
}

/** The label of the current page, for the capsule at the top. */
function pageLabel(pathname: string): { label: string; icon: typeof ShareNetwork } {
  const hit = navItems.find((i) => i.to === pathname)
  if (hit) return { label: hit.label, icon: hit.icon }
  return pathname === "/account"
    ? { label: "Account", icon: UserIcon }
    : { label: "Netscribe", icon: ShareNetwork }
}

function TopBar({
  pathname,
  onOpenMenu,
  menuOpen,
}: {
  pathname: string
  onOpenMenu: () => void
  menuOpen: boolean
}) {
  const { resolved } = useTheme()
  const { label, icon: Icon } = pageLabel(pathname)
  const dark = resolved === "dark"
  return (
    <header className="sticky top-0 z-20 flex items-center justify-between gap-3 border-b bg-background/70 px-4 py-3 backdrop-blur-xl md:border-b-0 md:bg-transparent md:px-8 md:pt-6 md:pb-0 md:backdrop-blur-none">
      <div className="flex items-center gap-3">
        <Button
          variant="outline"
          size="icon"
          className="md:hidden"
          aria-label="Open navigation"
          aria-expanded={menuOpen}
          onClick={onOpenMenu}
        >
          <span aria-hidden="true" className="relative size-5">
            <List
              className={cn(
                "absolute inset-0 size-5 transition-[opacity,transform]",
                menuOpen ? "scale-75 rotate-90 opacity-0" : "scale-100 rotate-0 opacity-100",
              )}
            />
            <X
              className={cn(
                "absolute inset-0 size-5 transition-[opacity,transform]",
                menuOpen ? "scale-100 rotate-0 opacity-100" : "scale-75 -rotate-90 opacity-0",
              )}
            />
          </span>
        </Button>
        <BrandMark className="md:hidden" />
        <div className="hidden items-center gap-2 rounded-full border bg-muted py-2 pr-4 pl-3 text-sm md:inline-flex">
          <Icon aria-hidden="true" className="size-4 text-brand" />
          <span className="font-medium">{label}</span>
        </div>
      </div>
      <Button
        variant="outline"
        size="icon"
        aria-label={dark ? "Switch to the light theme" : "Switch to the dark theme"}
        onClick={() => setTheme(dark ? "light" : "dark")}
        className="group"
      >
        {dark ? (
          <Sun
            aria-hidden="true"
            className="size-5 transition-transform duration-500 group-hover:rotate-45"
          />
        ) : (
          <Moon
            aria-hidden="true"
            className="size-5 transition-transform duration-500 group-hover:-rotate-12"
          />
        )}
      </Button>
    </header>
  )
}

function Layout() {
  const { state, signOut } = useSession()
  const location = useLocation()
  const [menuOpen, setMenuOpen] = useState(false)
  if (state.status !== "signedIn") return null
  const { user, permissions } = state.session
  // Leaving the page must not depend on the request: signOut forgets the session either way.
  const onSignOut = () => void signOut().catch(() => undefined)

  return (
    <div className="flex min-h-screen">
      {/* Below the md breakpoint the sidebar folds into a sheet behind a menu button. */}
      <Sheet open={menuOpen} onOpenChange={setMenuOpen}>
        <SheetContent className="right-auto left-0 w-72 justify-between border-r border-l-0 bg-sidebar/95 backdrop-blur-2xl">
          <SheetTitle className="sr-only">Navigation</SheetTitle>
          <SheetDescription className="sr-only">Pages of Netscribe</SheetDescription>
          <Sidebar
            user={user}
            permissions={permissions}
            onSignOut={onSignOut}
            onNavigate={() => setMenuOpen(false)}
          />
        </SheetContent>
      </Sheet>
      <aside className="sticky top-0 hidden h-screen w-64 shrink-0 flex-col justify-between border-r bg-sidebar/80 p-4 backdrop-blur-xl md:flex">
        <Sidebar user={user} permissions={permissions} onSignOut={onSignOut} />
      </aside>
      <VersionBadge permissions={permissions} />
      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar
          pathname={location.pathname}
          onOpenMenu={() => setMenuOpen(true)}
          menuOpen={menuOpen}
        />
        <main className="min-w-0 flex-1 p-4 md:px-8 md:pt-6 md:pb-10">
          {/* A new key per page resets the boundary and replays the entrance. */}
          <div key={location.pathname} className="page-enter">
            <ErrorBoundary>
              <Outlet />
            </ErrorBoundary>
          </div>
        </main>
      </div>
    </div>
  )
}

export const routes: RouteObject[] = [
  { path: "/login", element: <Login /> },
  {
    path: "/",
    element: (
      <RequireSession>
        <Layout />
      </RequireSession>
    ),
    children: [
      {
        index: true,
        element: (
          <RequirePermission permission="topology:read">
            <Topology />
          </RequirePermission>
        ),
      },
      {
        path: "overview",
        element: (
          <RequirePermission permission="topology:read">
            <Overview />
          </RequirePermission>
        ),
      },
      {
        path: "inventory",
        element: (
          <RequirePermission permission="topology:read">
            <Inventory />
          </RequirePermission>
        ),
      },
      {
        path: "services",
        element: (
          <RequirePermission permission="topology:read">
            <Services />
          </RequirePermission>
        ),
      },
      {
        path: "containers",
        element: (
          <RequirePermission permission="topology:read">
            <Containers />
          </RequirePermission>
        ),
      },
      {
        path: "changes",
        element: (
          <RequirePermission permission="topology:read">
            <Changes />
          </RequirePermission>
        ),
      },
      {
        path: "users",
        element: (
          <RequirePermission permission="users:manage">
            <Users />
          </RequirePermission>
        ),
      },
      {
        path: "audit",
        element: (
          <RequirePermission permission="audit:read">
            <Audit />
          </RequirePermission>
        ),
      },
      {
        path: "settings",
        element: (
          <RequirePermission permission="system:manage">
            <Settings />
          </RequirePermission>
        ),
      },
      { path: "account", element: <Account /> },
      {
        path: "*",
        element: <EmptyState title="Page not found" text="This page does not exist." />,
      },
    ],
  },
]

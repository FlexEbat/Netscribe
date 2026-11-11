import { List, Moon, ShareNetwork, Sun, User as UserIcon, X } from "@phosphor-icons/react"
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
import { navItems, type NavItem } from "@/lib/nav"
import { can, type Permission, type User } from "@/lib/permissions"
import { setTheme, useTheme } from "@/lib/theme"
import { cn } from "@/lib/utils"
import { Account } from "@/pages/Account"
import { Audit } from "@/pages/Audit"
import { Users } from "@/pages/Users"
import { Inventory } from "@/pages/Inventory"
import { Groups } from "@/pages/Groups"
import { Overview } from "@/pages/Overview"
import { Login } from "@/pages/Login"
import { Changes } from "@/pages/Changes"
import { Containers } from "@/pages/Containers"
import { Services } from "@/pages/Services"
import { ApiReference } from "@/pages/ApiReference"
import { Health } from "@/pages/Health"
import { Settings } from "@/pages/Settings"
import { Topology } from "@/pages/Topology"
import { t, type Key } from "@/i18n"

const groupLabels = {
  workspace: "nav.workspace",
  admin: "nav.administration",
} as const satisfies Record<string, Key>

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
      <p className="px-3 pb-2 text-xs font-medium tracking-wider text-muted-foreground uppercase">
        {t(groupLabels[group])}
      </p>
      {items.map(({ to, label, icon: Icon, tint }, index) => (
        <NavLink
          key={to}
          to={to}
          end
          onClick={onNavigate}
          style={{ "--i": offset + index, "--tile": tint } as CSSProperties}
          className={({ isActive }) =>
            cn(
              "nav-item group flex items-center gap-3 rounded-full p-1 pr-4 text-sm font-medium transition-[background-color,color] focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
              isActive
                ? "bg-foreground text-background"
                : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
            )
          }
        >
          {({ isActive }) => (
            <>
              <span
                aria-hidden="true"
                className={cn(
                  "nav-tile size-8 rounded-full transition-transform group-hover:scale-105",
                  isActive && "nav-tile-active",
                )}
              >
                <Icon className="size-4" weight={isActive ? "fill" : "duotone"} />
              </span>
              {t(label)}
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
      <div className="flex min-h-0 flex-col gap-8 overflow-y-auto">
        <Brand className="px-2 pt-1" />
        <nav aria-label={t("app.main")} className="flex flex-col gap-6">
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

/** The place of the current page, for the line at the top. */
function pageLabel(pathname: string): { group: Key | null; label: Key; icon: NavItem["icon"] } {
  const hit = navItems.find((i) => i.to === pathname)
  if (hit) return { group: groupLabels[hit.group], label: hit.label, icon: hit.icon }
  return pathname === "/account"
    ? { group: null, label: "nav.account", icon: UserIcon }
    : { group: null, label: "nav.netscribe", icon: ShareNetwork }
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
  const { group, label } = pageLabel(pathname)
  const dark = resolved === "dark"
  return (
    <header className="sticky top-0 z-20 flex items-center justify-between gap-3 border-b bg-background/80 px-4 py-3 backdrop-blur-xl md:border-b-0 md:bg-transparent md:px-8 md:pt-6 md:pb-0 md:backdrop-blur-none">
      <div className="flex items-center gap-3">
        <Button
          variant="outline"
          size="icon"
          className="md:hidden"
          aria-label={t("app.openNavigation")}
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
        <p className="hidden items-center gap-2 text-sm text-muted-foreground md:flex">
          {group ? (
            <>
              <span>{t(group)}</span>
              <span aria-hidden="true">/</span>
            </>
          ) : null}
          <span className="font-medium text-foreground">{t(label)}</span>
        </p>
      </div>
      <Button
        variant="outline"
        size="icon"
        aria-label={t(dark ? "app.switchToLight" : "app.switchToDark")}
        onClick={() => setTheme(dark ? "light" : "dark")}
        className="group rounded-full"
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
          <SheetTitle className="sr-only">{t("app.navigation")}</SheetTitle>
          <SheetDescription className="sr-only">{t("app.pagesOfNetscribe")}</SheetDescription>
          <Sidebar
            user={user}
            permissions={permissions}
            onSignOut={onSignOut}
            onNavigate={() => setMenuOpen(false)}
          />
        </SheetContent>
      </Sheet>
      <aside className="sticky top-0 hidden h-screen w-72 shrink-0 flex-col justify-between gap-6 border-r bg-sidebar/90 p-4 backdrop-blur-xl md:flex">
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
        path: "groups",
        element: (
          <RequirePermission permission="topology:read">
            <Groups />
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
        path: "reference",
        element: (
          <RequirePermission permission="topology:read">
            <ApiReference />
          </RequirePermission>
        ),
      },
      {
        path: "health",
        element: (
          <RequirePermission permission="system:manage">
            <Health />
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
        element: <EmptyState title={t("app.pageNotFound")} text={t("app.pageNotFoundText")} />,
      },
    ],
  },
]

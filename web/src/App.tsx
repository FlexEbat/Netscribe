import { useState } from "react"
import { NavLink, Outlet, useLocation, type RouteObject } from "react-router-dom"
import {
  Box,
  Cable,
  History,
  List,
  Menu,
  Network,
  ScrollText,
  Users as UsersIcon,
} from "lucide-react"
import { useSession } from "@/api/session"
import { EmptyState } from "@/components/EmptyState"
import { ErrorBoundary } from "@/components/ErrorBoundary"
import { RequirePermission } from "@/components/RequirePermission"
import { RequireSession } from "@/components/RequireSession"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet"
import { UserMenu } from "@/components/UserMenu"
import { can, type Permission, type User } from "@/lib/permissions"
import { cn } from "@/lib/utils"
import { Account } from "@/pages/Account"
import { Audit } from "@/pages/Audit"
import { Users } from "@/pages/Users"
import { Inventory } from "@/pages/Inventory"
import { Login } from "@/pages/Login"
import { Changes } from "@/pages/Changes"
import { Containers } from "@/pages/Containers"
import { Services } from "@/pages/Services"
import { Topology } from "@/pages/Topology"

// Pages are added here as slices deliver them.
const navItems: { to: string; label: string; icon: typeof Network; permission: Permission }[] = [
  { to: "/", label: "Topology", icon: Network, permission: "topology:read" },
  { to: "/inventory", label: "Inventory", icon: List, permission: "topology:read" },
  { to: "/services", label: "Services", icon: Cable, permission: "topology:read" },
  { to: "/containers", label: "Containers", icon: Box, permission: "topology:read" },
  { to: "/changes", label: "Changes", icon: History, permission: "topology:read" },
  { to: "/users", label: "Users", icon: UsersIcon, permission: "users:manage" },
  { to: "/audit", label: "Audit log", icon: ScrollText, permission: "audit:read" },
]

interface SidebarProps {
  user: User
  permissions: Permission[]
  onSignOut: () => void
  onNavigate?: () => void
}

function Sidebar({ user, permissions, onSignOut, onNavigate }: SidebarProps) {
  return (
    <>
      <div>
        <p className="mb-6 px-2 text-lg font-semibold">Netscribe</p>
        <nav aria-label="Main" className="flex flex-col gap-1">
          {navItems
            .filter((item) => can(permissions, item.permission))
            .map(({ to, label, icon: Icon }) => (
              <NavLink
                key={to}
                to={to}
                end
                onClick={onNavigate}
                className={({ isActive }) =>
                  cn(
                    "flex items-center gap-2 rounded-lg px-2 py-1.5 text-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
                    isActive
                      ? "bg-accent font-medium text-accent-foreground"
                      : "text-muted-foreground",
                  )
                }
              >
                <Icon aria-hidden="true" className="size-4" />
                {label}
              </NavLink>
            ))}
        </nav>
      </div>
      <UserMenu user={user} onSignOut={onSignOut} />
    </>
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
    <div className="flex min-h-screen flex-col md:flex-row">
      {/* Below the md breakpoint the sidebar folds into a sheet behind a menu button. */}
      <header className="flex items-center justify-between border-b bg-muted px-4 py-2 md:hidden">
        <p className="text-lg font-semibold">Netscribe</p>
        <Button
          variant="ghost"
          size="sm"
          aria-label="Open navigation"
          aria-expanded={menuOpen}
          onClick={() => setMenuOpen(true)}
        >
          <Menu aria-hidden="true" className="size-5" />
        </Button>
      </header>
      <Sheet open={menuOpen} onOpenChange={setMenuOpen}>
        <SheetContent className="right-auto left-0 w-64 justify-between border-r border-l-0 bg-muted">
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
      <aside className="hidden w-56 shrink-0 flex-col justify-between border-r bg-muted p-4 md:flex">
        <Sidebar user={user} permissions={permissions} onSignOut={onSignOut} />
      </aside>
      <main className="min-w-0 flex-1 p-4 md:p-6">
        {/* A new key per page resets the boundary when the user navigates away. */}
        <ErrorBoundary key={location.pathname}>
          <Outlet />
        </ErrorBoundary>
      </main>
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
      { path: "account", element: <Account /> },
      {
        path: "*",
        element: <EmptyState title="Page not found" text="This page does not exist." />,
      },
    ],
  },
]

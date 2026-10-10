import { NavLink, Outlet, type RouteObject } from "react-router-dom"
import { List, Network } from "lucide-react"
import { useSession } from "@/api/session"
import { EmptyState } from "@/components/EmptyState"
import { RequirePermission } from "@/components/RequirePermission"
import { RequireSession } from "@/components/RequireSession"
import { UserMenu } from "@/components/UserMenu"
import { can, type Permission } from "@/lib/permissions"
import { cn } from "@/lib/utils"
import { Account } from "@/pages/Account"
import { Inventory } from "@/pages/Inventory"
import { Login } from "@/pages/Login"
import { Topology } from "@/pages/Topology"

// Pages are added here as slices deliver them.
const navItems: { to: string; label: string; icon: typeof Network; permission: Permission }[] = [
  { to: "/", label: "Topology", icon: Network, permission: "topology:read" },
  { to: "/inventory", label: "Inventory", icon: List, permission: "topology:read" },
]

function Layout() {
  const { state, signOut } = useSession()
  if (state.status !== "signedIn") return null
  const { user, permissions } = state.session
  // Leaving the page must not depend on the request: signOut forgets the session either way.
  const onSignOut = () => void signOut().catch(() => undefined)

  return (
    <div className="flex min-h-screen">
      <aside className="flex w-56 shrink-0 flex-col justify-between border-r bg-muted p-4">
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
      </aside>
      <main className="flex-1 p-6">
        <Outlet />
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
      { path: "account", element: <Account /> },
      {
        path: "*",
        element: <EmptyState title="Page not found" text="This page does not exist." />,
      },
    ],
  },
]

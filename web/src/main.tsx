import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { createBrowserRouter, NavLink, Outlet, RouterProvider } from "react-router-dom"
import { Network } from "lucide-react"
import { EmptyState } from "@/components/EmptyState"
import { cn } from "@/lib/utils"
import { Topology } from "@/pages/Topology"
import "./index.css"

// Pages are added here as slices deliver them.
const navItems = [{ to: "/", label: "Topology", icon: Network }]

function Layout() {
  return (
    <div className="flex min-h-screen">
      <aside className="w-56 shrink-0 border-r bg-muted p-4">
        <p className="mb-6 px-2 text-lg font-semibold">Netscribe</p>
        <nav aria-label="Main" className="flex flex-col gap-1">
          {navItems.map(({ to, label, icon: Icon }) => (
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
      </aside>
      <main className="flex-1 p-6">
        <Outlet />
      </main>
    </div>
  )
}

const router = createBrowserRouter([
  {
    path: "/",
    element: <Layout />,
    children: [
      { index: true, element: <Topology /> },
      {
        path: "*",
        element: <EmptyState title="Page not found" text="This page does not exist." />,
      },
    ],
  },
])

const root = document.getElementById("root")
if (!root) throw new Error("missing #root element")

createRoot(root).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>,
)

import { StrictMode, useEffect } from "react"
import { createRoot } from "react-dom/client"
import { createBrowserRouter, RouterProvider } from "react-router-dom"
import { toast } from "sonner"
import { openEvents } from "@/api/events"
import { SessionProvider, useSession } from "@/api/session"
import { routes } from "@/App"
import { AmbientBackground } from "@/components/AmbientBackground"
import { Toaster } from "@/components/ui/sonner"
import { can } from "@/lib/permissions"
import { startSpotlight } from "@/lib/spotlight"
import { initTheme } from "@/lib/theme"
import "@fontsource-variable/geist"
import "@fontsource-variable/geist-mono"
import "./index.css"

initTheme()
startSpotlight()

const router = createBrowserRouter(routes)

/**
 * Tells administrators when the server reloaded its config or refused a file. The server
 * sends these events only to users with users:manage, the check here keeps other roles
 * from opening a stream that would carry nothing for them.
 */
function ConfigToasts() {
  const { state } = useSession()
  const allowed = state.status === "signedIn" && can(state.session.permissions, "users:manage")
  useEffect(() => {
    if (!allowed) return
    return openEvents({
      "config.reloaded": () => toast.success("Config reloaded"),
      "config.error": ({ error }) => toast.error("Config not reloaded", { description: error }),
    })
  }, [allowed])
  return null
}

const root = document.getElementById("root")
if (!root) throw new Error("missing #root element")

createRoot(root).render(
  <StrictMode>
    <SessionProvider>
      <AmbientBackground />
      <ConfigToasts />
      <RouterProvider router={router} />
      <Toaster />
    </SessionProvider>
  </StrictMode>,
)

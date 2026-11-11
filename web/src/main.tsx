import { StrictMode, useEffect } from "react"
import { createRoot } from "react-dom/client"
import { createBrowserRouter, RouterProvider } from "react-router-dom"
import { toast } from "sonner"
import { openEvents } from "@/api/events"
import { SessionProvider, useSession } from "@/api/session"
import { routes } from "@/App"
import { Toaster } from "@/components/ui/sonner"
import { can } from "@/lib/permissions"
import { initTheme } from "@/lib/theme"
import { t } from "@/i18n"
import { I18nRoot } from "@/i18n/root"
import "@fontsource-variable/geist"
import "@fontsource-variable/geist-mono"
import "@fontsource-variable/fraunces/opsz.css"
import "./index.css"

initTheme()

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
      "config.reloaded": () => toast.success(t("main.configReloaded")),
      "config.error": ({ error }) =>
        toast.error(t("main.configNotReloaded"), { description: error }),
    })
  }, [allowed])
  return null
}

const root = document.getElementById("root")
if (!root) throw new Error("missing #root element")

createRoot(root).render(
  <StrictMode>
    <I18nRoot>
      <SessionProvider>
        <ConfigToasts />
        <RouterProvider router={router} />
        <Toaster />
      </SessionProvider>
    </I18nRoot>
  </StrictMode>,
)

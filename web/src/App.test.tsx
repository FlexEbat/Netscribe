import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { createMemoryRouter, RouterProvider } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { SessionProvider } from "@/api/session"
import { routes } from "@/App"
import { setCsrfToken } from "@/api/client"
import type { Role } from "@/lib/permissions"
import { json, makeUser, meBody, mockFetch, type FetchMock } from "@/test-utils"

afterEach(() => {
  setCsrfToken("")
  vi.unstubAllGlobals()
})

function renderApp(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  render(
    <SessionProvider>
      <RouterProvider router={router} />
    </SessionProvider>,
  )
  return router
}

const anonymous = { "GET /api/auth/me": () => json(401, { error: "authentication required" }) }

describe("routing", () => {
  it("sends a signed-out visitor to the login page and remembers where they wanted to go", async () => {
    mockFetch({
      ...anonymous,
      "POST /api/auth/login": () => json(200, meBody(makeUser())),
    })
    const router = renderApp("/account")
    expect(await screen.findByRole("button", { name: "Sign in" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/login")

    const user = userEvent.setup()
    await user.type(screen.getByLabelText("Username"), "alice")
    await user.type(screen.getByLabelText("Password"), "correct horse battery")
    await user.click(screen.getByRole("button", { name: "Sign in" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/account"))
  })

  it("shows the topology page and the user menu to a signed-in user", async () => {
    mockFetch({ "GET /api/auth/me": () => json(200, meBody(makeUser({ role: "operator" }))) })
    renderApp("/")
    expect(await screen.findByText("No scans yet")).toBeInTheDocument()
    const nav = screen.getByRole("navigation", { name: "Main" })
    expect(within(nav).getByRole("link", { name: "Topology" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: /alice/ })).toBeInTheDocument()
    expect(screen.getByText("Operator")).toBeInTheDocument()
  })

  it("hides navigation links the role cannot use", async () => {
    mockFetch({ "GET /api/auth/me": () => json(200, meBody(makeUser(), [])) })
    renderApp("/")
    expect(await screen.findByText("No access")).toBeInTheDocument()
    expect(screen.queryByRole("link", { name: "Topology" })).not.toBeInTheDocument()
  })

  it("keeps a user who must change the password on the account page", async () => {
    mockFetch({
      "GET /api/auth/me": () => json(200, meBody(makeUser({ mustChangePassword: true }))),
    })
    const router = renderApp("/")
    expect(
      await screen.findByText("You must set a new password before you can continue."),
    ).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/account")
  })

  it("shows a not-found page for an unknown route", async () => {
    mockFetch({ "GET /api/auth/me": () => json(200, meBody(makeUser())) })
    renderApp("/nowhere")
    expect(await screen.findByText("Page not found")).toBeInTheDocument()
  })

  it("redirects the login page to the app when already signed in", async () => {
    mockFetch({ "GET /api/auth/me": () => json(200, meBody(makeUser())) })
    const router = renderApp("/login")
    await waitFor(() => expect(router.state.location.pathname).toBe("/"))
  })
})

describe("signing out", () => {
  it("calls the server with the CSRF token and returns to the login page", async () => {
    const mock = mockFetch({
      "GET /api/auth/me": () => json(200, meBody(makeUser(), undefined, "token-xyz")),
      "POST /api/auth/logout": () => json(204),
    })
    const router = renderApp("/")
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: /alice/ }))
    await user.click(await screen.findByRole("menuitem", { name: /Sign out/ }))

    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    const logout = mock.calls.find((c) => c.path === "/api/auth/logout")
    expect((logout?.init.headers as Record<string, string>)["X-CSRF-Token"]).toBe("token-xyz")
  })

  it("leaves even when the server cannot be reached", async () => {
    mockFetch({
      "GET /api/auth/me": () => json(200, meBody(makeUser())),
      "POST /api/auth/logout": () => json(500, { error: "internal error" }),
    })
    const router = renderApp("/")
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: /alice/ }))
    await user.click(await screen.findByRole("menuitem", { name: /Sign out/ }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
  })

  it("signs the user out when any request gets 401, such as after the session expired", async () => {
    let expired = false
    const mock: FetchMock = mockFetch({
      "GET /api/auth/me": () => json(200, meBody(makeUser())),
      "PUT /api/auth/password": () => {
        expired = true
        return json(401, { error: "authentication required" })
      },
    })
    const router = renderApp("/account")
    const user = userEvent.setup()
    await user.type(await screen.findByLabelText("Current password"), "correct horse battery")
    await user.type(screen.getByLabelText("New password"), "a brand new passphrase")
    await user.type(screen.getByLabelText("Repeat the new password"), "a brand new passphrase")
    await user.click(screen.getByRole("button", { name: "Change password" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(expired).toBe(true)
    expect(mock.calls.length).toBeGreaterThan(1)
  })
})

describe("role badge", () => {
  it.each<[Role, string]>([
    ["viewer", "Viewer"],
    ["operator", "Operator"],
    ["admin", "Administrator"],
  ])("labels a %s", async (role, label) => {
    mockFetch({ "GET /api/auth/me": () => json(200, meBody(makeUser({ role }))) })
    renderApp("/")
    expect(await screen.findByText(label)).toBeInTheDocument()
  })
})

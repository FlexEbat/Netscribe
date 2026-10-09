import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { createMemoryRouter, RouterProvider } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { setCsrfToken } from "@/api/client"
import { SessionProvider } from "@/api/session"
import { routes } from "@/App"
import { json, makeUser, meBody, mockFetch } from "@/test-utils"

afterEach(() => {
  setCsrfToken("")
  vi.unstubAllGlobals()
})

async function openLogin(login: () => Response) {
  const mock = mockFetch({
    "GET /api/auth/me": () => json(401, { error: "authentication required" }),
    "POST /api/auth/login": login,
  })
  const router = createMemoryRouter(routes, { initialEntries: ["/login"] })
  render(
    <SessionProvider>
      <RouterProvider router={router} />
    </SessionProvider>,
  )
  const user = userEvent.setup()
  await screen.findByRole("button", { name: "Sign in" })
  return { user, mock, router }
}

async function fill(user: ReturnType<typeof userEvent.setup>, name: string, password: string) {
  await user.type(screen.getByLabelText("Username"), name)
  await user.type(screen.getByLabelText("Password"), password)
  await user.click(screen.getByRole("button", { name: "Sign in" }))
}

describe("Login", () => {
  it("labels both fields and puts the cursor in the first one", async () => {
    await openLogin(() => json(200, meBody(makeUser())))
    expect(screen.getByLabelText("Username")).toHaveFocus()
    expect(screen.getByLabelText("Password")).toHaveAttribute("type", "password")
    expect(screen.getByLabelText("Password")).toHaveAttribute("autocomplete", "current-password")
    expect(screen.getByLabelText("Username")).toHaveAttribute("autocomplete", "username")
  })

  it("signs in and opens the app", async () => {
    const { user, mock, router } = await openLogin(() => json(200, meBody(makeUser())))
    await fill(user, "alice", "correct horse battery")
    await waitFor(() => expect(router.state.location.pathname).toBe("/"))
    const call = mock.calls.find((c) => c.path === "/api/auth/login")!
    expect(JSON.parse(String(call.init.body))).toEqual({
      username: "alice",
      password: "correct horse battery",
    })
  })

  it("shows one message for a rejected sign-in and clears the password", async () => {
    const { user } = await openLogin(() => json(401, { error: "invalid username or password" }))
    await fill(user, "alice", "wrong wrong wrong")
    expect(await screen.findByRole("alert")).toHaveTextContent("Invalid username or password.")
    expect(screen.getByLabelText("Password")).toHaveValue("")
    expect(screen.getByLabelText("Username")).toHaveValue("alice")
  })

  it("tells the user how long to wait after 429", async () => {
    const { user } = await openLogin(() =>
      json(429, { error: "too many attempts" }, { "Retry-After": "37" }),
    )
    await fill(user, "alice", "whatever password")
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Too many attempts. Try again in 37 seconds.",
    )
  })

  it("does not show server internals for an unexpected failure", async () => {
    const { user } = await openLogin(() => json(500, { error: "sql: database is locked" }))
    await fill(user, "alice", "whatever password")
    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("Sign-in failed. Try again later.")
    expect(alert).not.toHaveTextContent("sql")
  })

  it("disables the button while the request runs", async () => {
    let release: (r: Response) => void = () => {}
    const pending = new Promise<Response>((resolve) => (release = resolve))
    mockFetch({
      "GET /api/auth/me": () => json(401, { error: "authentication required" }),
      "POST /api/auth/login": () => pending as unknown as Response,
    })
    const router = createMemoryRouter(routes, { initialEntries: ["/login"] })
    render(
      <SessionProvider>
        <RouterProvider router={router} />
      </SessionProvider>,
    )
    const user = userEvent.setup()
    await screen.findByRole("button", { name: "Sign in" })
    await user.type(screen.getByLabelText("Username"), "alice")
    await user.type(screen.getByLabelText("Password"), "correct horse battery")
    await user.click(screen.getByRole("button", { name: "Sign in" }))
    expect(await screen.findByRole("button", { name: "Signing in…" })).toBeDisabled()
    release(json(401, { error: "invalid username or password" }))
    await waitFor(() => expect(screen.getByRole("button", { name: "Sign in" })).toBeEnabled())
  })
})

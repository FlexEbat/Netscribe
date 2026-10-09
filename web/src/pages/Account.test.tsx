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

async function openAccount(handlers: Parameters<typeof mockFetch>[0] = {}, mustChange = false) {
  const mock = mockFetch({
    "GET /api/auth/me": () =>
      json(200, meBody(makeUser({ mustChangePassword: mustChange }), undefined, "csrf-9")),
    ...handlers,
  })
  const router = createMemoryRouter(routes, { initialEntries: ["/account"] })
  render(
    <SessionProvider>
      <RouterProvider router={router} />
    </SessionProvider>,
  )
  await screen.findByLabelText("Current password")
  return { user: userEvent.setup(), mock, router }
}

async function fillPasswords(
  user: ReturnType<typeof userEvent.setup>,
  current: string,
  next: string,
  repeat: string,
) {
  await user.type(screen.getByLabelText("Current password"), current)
  await user.type(screen.getByLabelText("New password"), next)
  await user.type(screen.getByLabelText("Repeat the new password"), repeat)
  await user.click(screen.getByRole("button", { name: "Change password" }))
}

describe("Account", () => {
  it("shows who is signed in", async () => {
    await openAccount()
    expect(screen.getByText("Signed in as alice")).toBeInTheDocument()
    // The role shows in the account card and in the user menu.
    expect(screen.getAllByText("Viewer")).toHaveLength(2)
    expect(
      screen.queryByText("You must set a new password before you can continue."),
    ).not.toBeInTheDocument()
  })

  it("does not send the request when the new passwords differ", async () => {
    const { user, mock } = await openAccount()
    await fillPasswords(
      user,
      "correct horse battery",
      "a brand new passphrase",
      "a different passphrase",
    )
    expect(await screen.findByRole("alert")).toHaveTextContent("The new passwords do not match.")
    expect(mock.calls.some((c) => c.method === "PUT")).toBe(false)
  })

  it("changes the password, then asks for a new sign-in", async () => {
    const { user, mock, router } = await openAccount({ "PUT /api/auth/password": () => json(204) })
    await fillPasswords(
      user,
      "correct horse battery",
      "a brand new passphrase",
      "a brand new passphrase",
    )

    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(
      await screen.findByText("Your password was changed. Sign in with the new one."),
    ).toBeInTheDocument()
    const call = mock.calls.find((c) => c.method === "PUT")!
    expect(JSON.parse(String(call.init.body))).toEqual({
      currentPassword: "correct horse battery",
      newPassword: "a brand new passphrase",
    })
    expect((call.init.headers as Record<string, string>)["X-CSRF-Token"]).toBe("csrf-9")
  })

  it("shows the server's reason when the password is refused", async () => {
    const { user, router } = await openAccount({
      "PUT /api/auth/password": () => json(400, { error: "current password is incorrect" }),
    })
    await fillPasswords(
      user,
      "wrong wrong wrong",
      "a brand new passphrase",
      "a brand new passphrase",
    )
    expect(await screen.findByRole("alert")).toHaveTextContent("current password is incorrect")
    expect(router.state.location.pathname).toBe("/account")
    expect(screen.getByRole("button", { name: "Change password" })).toBeEnabled()
  })

  it("hides unexpected server errors behind a generic message", async () => {
    const { user } = await openAccount({
      "PUT /api/auth/password": () => json(500, { error: "sql: no such table" }),
    })
    await fillPasswords(
      user,
      "correct horse battery",
      "a brand new passphrase",
      "a brand new passphrase",
    )
    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("The password was not changed. Try again later.")
    expect(alert).not.toHaveTextContent("sql")
  })

  it("explains why the user is here when the password must be changed", async () => {
    await openAccount({}, true)
    expect(
      screen.getByText("You must set a new password before you can continue."),
    ).toBeInTheDocument()
  })
})

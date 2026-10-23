import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { createMemoryRouter, RouterProvider } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { setCsrfToken, type AccessToken, type AuditEntry } from "@/api/client"
import { SessionProvider } from "@/api/session"
import { routes } from "@/App"
import type { Role, User } from "@/lib/permissions"
import { json, makeUser, meBody, mockFetch } from "@/test-utils"

afterEach(() => {
  setCsrfToken("")
  vi.unstubAllGlobals()
})

function open(
  role: Role,
  entry: string,
  handlers: Record<string, () => Response | Promise<Response>> = {},
) {
  vi.stubGlobal("EventSource", undefined)
  const mock = mockFetch({
    "GET /api/auth/me": () => json(200, meBody(makeUser({ role }))),
    ...handlers,
  })
  const router = createMemoryRouter(routes, { initialEntries: [entry] })
  render(
    <SessionProvider>
      <RouterProvider router={router} />
    </SessionProvider>,
  )
  return mock
}

function body(mock: ReturnType<typeof open>, method: string, path: string): unknown {
  const call = [...mock.calls].reverse().find((c) => c.method === method && c.path === path)
  return call ? JSON.parse(String(call.init.body ?? "null")) : undefined
}

const users: User[] = [
  makeUser({ id: 1, username: "alice", role: "admin" }),
  makeUser({ id: 2, username: "olga", role: "operator", mustChangePassword: true }),
  makeUser({ id: 3, username: "vera", role: "viewer", disabled: true }),
]

describe("Users page", () => {
  it("is closed to roles without users:manage", async () => {
    open("operator", "/users")
    expect(await screen.findByText(/no access/i)).toBeInTheDocument()
  })

  it("lists the users with role and state", async () => {
    open("admin", "/users", { "GET /api/users": () => json(200, { users }) })
    const table = await screen.findByRole("table")
    expect(within(table).getByText("olga")).toBeInTheDocument()
    expect(within(table).getByText("Must change password")).toBeInTheDocument()
    expect(within(table).getByText("Disabled")).toBeInTheDocument()
  })

  it("creates a user with a temporary password", async () => {
    const mock = open("admin", "/users", {
      "GET /api/users": () => json(200, { users }),
      "POST /api/users": () => json(201, { user: makeUser({ id: 9, username: "newbie" }) }),
    })
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "Add user" }))
    await user.type(await screen.findByLabelText("Username"), "newbie")
    await user.selectOptions(screen.getByLabelText("Role"), "operator")
    await user.type(screen.getByLabelText("Temporary password"), "correct horse battery")
    await user.click(screen.getByRole("button", { name: "Create user" }))
    await waitFor(() =>
      expect(body(mock, "POST", "/api/users")).toEqual({
        username: "newbie",
        role: "operator",
        password: "correct horse battery",
      }),
    )
  })

  it("shows the message of a rejected user", async () => {
    open("admin", "/users", {
      "GET /api/users": () => json(200, { users }),
      "POST /api/users": () => json(400, { error: "username is invalid" }),
    })
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "Add user" }))
    await user.click(await screen.findByRole("button", { name: "Create user" }))
    expect(await within(screen.getByRole("dialog")).findByRole("alert")).toHaveTextContent(
      "username is invalid",
    )
  })

  it("changes a role and tells the user when the last administrator is protected", async () => {
    const mock = open("admin", "/users", {
      "GET /api/users": () => json(200, { users }),
      "PATCH /api/users/1": () => json(409, { error: "cannot remove the last administrator" }),
    })
    await userEvent.setup().selectOptions(await screen.findByLabelText("Role of alice"), "viewer")
    expect(await screen.findByRole("alert")).toHaveTextContent("The last administrator cannot be")
    expect(body(mock, "PATCH", "/api/users/1")).toEqual({ role: "viewer" })
  })

  it("disables an account", async () => {
    const mock = open("admin", "/users", {
      "GET /api/users": () => json(200, { users }),
      "PATCH /api/users/2": () => json(200, { user: users[1] }),
    })
    const row = (await screen.findByText("olga")).closest("tr") as HTMLElement
    await userEvent.setup().click(within(row).getByRole("button", { name: "Disable" }))
    await waitFor(() => expect(body(mock, "PATCH", "/api/users/2")).toEqual({ disabled: true }))
  })

  it("resets a password", async () => {
    const mock = open("admin", "/users", {
      "GET /api/users": () => json(200, { users }),
      "PUT /api/users/2/password": () => new Response(null, { status: 204 }),
    })
    const user = userEvent.setup()
    const row = (await screen.findByText("olga")).closest("tr") as HTMLElement
    await user.click(within(row).getByRole("button", { name: "Reset password" }))
    await user.type(await screen.findByLabelText("Temporary password"), "a brand new password")
    await user.click(screen.getByRole("button", { name: "Set password" }))
    await waitFor(() =>
      expect(body(mock, "PUT", "/api/users/2/password")).toEqual({
        password: "a brand new password",
      }),
    )
  })

  it("deletes only after a confirmation", async () => {
    const mock = open("admin", "/users", {
      "GET /api/users": () => json(200, { users }),
      "DELETE /api/users/3": () => new Response(null, { status: 204 }),
    })
    const user = userEvent.setup()
    const row = (await screen.findByText("vera")).closest("tr") as HTMLElement
    await user.click(within(row).getByRole("button", { name: "Delete" }))
    expect(await screen.findByRole("alertdialog")).toHaveTextContent("Delete vera?")
    expect(mock.calls.some((c) => c.method === "DELETE")).toBe(false)
    await user.click(
      within(screen.getByRole("alertdialog")).getByRole("button", { name: "Delete" }),
    )
    await waitFor(() => expect(mock.calls.some((c) => c.path === "/api/users/3")).toBe(true))
  })
})

function entry(id: number, action: string, result: AuditEntry["result"] = "ok"): AuditEntry {
  return {
    id,
    at: "2026-10-09T12:00:00.000Z",
    userId: 1,
    username: "alice",
    action,
    entity: "",
    entityId: "",
    result,
    ip: "192.168.1.5",
    detail: `detail ${id}`,
  }
}

describe("Audit page", () => {
  it("is closed to roles without audit:read", async () => {
    open("operator", "/audit")
    expect(await screen.findByText(/no access/i)).toBeInTheDocument()
  })

  it("lists entries and filters them on the server", async () => {
    const mock = open("admin", "/audit", {
      "GET /api/audit?limit=50": () =>
        json(200, { entries: [entry(2, "logout"), entry(1, "login")] }),
      "GET /api/audit?limit=50&user=alice&action=login&result=ok&from=2026-10-01&to=2026-10-31":
        () => json(200, { entries: [entry(1, "login")] }),
    })
    const user = userEvent.setup()
    await screen.findByText("detail 2")
    await user.type(screen.getByLabelText("User"), "alice")
    await user.selectOptions(screen.getByLabelText("Action"), "login")
    await user.selectOptions(screen.getByLabelText("Result"), "ok")
    await user.type(screen.getByLabelText("From"), "2026-10-01")
    await user.type(screen.getByLabelText("To"), "2026-10-31")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    await waitFor(() => expect(screen.queryByText("detail 2")).not.toBeInTheDocument())
    expect(screen.getByText("detail 1")).toBeInTheDocument()
    expect(mock.calls.some((c) => c.path.includes("user=alice"))).toBe(true)
  })

  it("loads the next page from the oldest entry shown", async () => {
    const page = Array.from({ length: 50 }, (_, i) => entry(100 - i, "login"))
    const mock = open("admin", "/audit", {
      "GET /api/audit?limit=50": () => json(200, { entries: page }),
      "GET /api/audit?limit=50&before=51": () => json(200, { entries: [entry(50, "logout")] }),
    })
    await userEvent.setup().click(await screen.findByRole("button", { name: "Load more" }))
    expect(await screen.findByText("detail 50")).toBeInTheDocument()
    expect(mock.calls.some((c) => c.path === "/api/audit?limit=50&before=51")).toBe(true)
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument()
  })

  it("drops a late page of the old filters instead of mixing it into the new list", async () => {
    let release!: () => void
    const gate = new Promise<void>((resolve) => (release = resolve))
    const page = Array.from({ length: 50 }, (_, i) => entry(100 - i, "login"))
    open("admin", "/audit", {
      "GET /api/audit?limit=50": () => json(200, { entries: page }),
      "GET /api/audit?limit=50&before=51": async () => {
        await gate
        return json(200, { entries: [entry(50, "logout")] })
      },
      "GET /api/audit?limit=50&user=alice": () => json(200, { entries: [entry(1, "login")] }),
    })
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "Load more" }))
    await user.type(screen.getByLabelText("User"), "alice")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    await screen.findByText("detail 1")

    release()
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(screen.queryByText("detail 50")).not.toBeInTheDocument()
    expect(screen.getByText("detail 1")).toBeInTheDocument()
    expect(screen.queryByText("detail 100")).not.toBeInTheDocument()
  })

  it("has no way to change an entry", async () => {
    open("admin", "/audit", {
      "GET /api/audit?limit=50": () => json(200, { entries: [entry(1, "login")] }),
    })
    await screen.findByText("detail 1")
    expect(screen.queryByRole("button", { name: /delete|edit|clear/i })).not.toBeInTheDocument()
  })

  it("shows a generic message when the log cannot be read", async () => {
    open("admin", "/audit", {
      "GET /api/audit?limit=50": () => json(500, { error: "disk I/O error" }),
    })
    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("The audit log could not be loaded.")
    expect(alert).not.toHaveTextContent("disk")
  })
})

function token(overrides: Partial<AccessToken> = {}): AccessToken {
  return {
    id: 1,
    name: "ci",
    prefix: "ndt_abcd",
    scope: "read",
    createdAt: "2026-10-01T00:00:00.000Z",
    expiresAt: "2099-01-01T00:00:00.000Z",
    lastUsedAt: null,
    revoked: false,
    ...overrides,
  }
}

describe("Access tokens on the account page", () => {
  it("lists tokens by prefix, with revoked ones marked and without a revoke button", async () => {
    open("operator", "/account", {
      "GET /api/tokens": () =>
        json(200, {
          tokens: [token(), token({ id: 2, name: "old", prefix: "ndt_wxyz", revoked: true })],
        }),
    })
    expect(await screen.findByText("ndt_abcd…")).toBeInTheDocument()
    expect(screen.getByText(/Revoked/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Revoke ci" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Revoke old" })).not.toBeInTheDocument()
  })

  it("shows the new token once and never again", async () => {
    const mock = open("operator", "/account", {
      "GET /api/tokens": () => json(200, { tokens: [] }),
      "POST /api/tokens": () =>
        json(201, { value: "ndt_SECRETSECRETSECRET", token: token({ scope: "write" }) }),
    })
    const user = userEvent.setup()
    await user.type(await screen.findByLabelText("Token name"), "script")
    await user.selectOptions(screen.getByLabelText("Scope"), "write")
    await user.click(screen.getByRole("button", { name: "Create token" }))
    expect(await screen.findByText("ndt_SECRETSECRETSECRET")).toBeInTheDocument()
    expect(body(mock, "POST", "/api/tokens")).toEqual({ name: "script", scope: "write", days: 90 })

    await user.click(screen.getByRole("button", { name: "Done" }))
    await waitFor(() =>
      expect(screen.queryByText("ndt_SECRETSECRETSECRET")).not.toBeInTheDocument(),
    )
  })

  describe("copying a new token", () => {
    async function showNewToken() {
      open("operator", "/account", {
        "GET /api/tokens": () => json(200, { tokens: [] }),
        "POST /api/tokens": () => json(201, { value: "ndt_SECRETSECRETSECRET", token: token() }),
      })
      const user = userEvent.setup()
      await user.type(await screen.findByLabelText("Token name"), "script")
      await user.click(screen.getByRole("button", { name: "Create token" }))
      await screen.findByText("ndt_SECRETSECRETSECRET")
      return { user, dialog: within(screen.getByRole("dialog")) }
    }
    // userEvent installs its own clipboard, so the stub goes in after setup().
    const useClipboard = (clipboard: unknown) =>
      Object.defineProperty(navigator, "clipboard", { value: clipboard, configurable: true })

    it("copies the token and says so", async () => {
      const { user, dialog } = await showNewToken()
      const writeText = vi.fn().mockResolvedValue(undefined)
      useClipboard({ writeText })
      await user.click(dialog.getByRole("button", { name: "Copy" }))
      expect(writeText).toHaveBeenCalledWith("ndt_SECRETSECRETSECRET")
      expect(await dialog.findByRole("button", { name: "Copied" })).toBeInTheDocument()
      expect(dialog.queryByRole("alert")).not.toBeInTheDocument()
    })

    it("tells the user to copy by hand when the browser refuses", async () => {
      const { user, dialog } = await showNewToken()
      useClipboard({ writeText: vi.fn().mockRejectedValue(new DOMException("denied")) })
      await user.click(dialog.getByRole("button", { name: "Copy" }))
      expect(await dialog.findByRole("alert")).toHaveTextContent("copy it by hand")
      expect(dialog.getByRole("button", { name: "Copy" })).toBeInTheDocument()
    })

    it("tells the user the same on plain HTTP, where there is no clipboard at all", async () => {
      const { user, dialog } = await showNewToken()
      useClipboard(undefined)
      await user.click(dialog.getByRole("button", { name: "Copy" }))
      expect(await dialog.findByRole("alert")).toHaveTextContent("copy it by hand")
    })
  })

  it("offers a viewer no write scope", async () => {
    open("viewer", "/account", { "GET /api/tokens": () => json(200, { tokens: [] }) })
    const scope = await screen.findByLabelText("Scope")
    expect(within(scope).queryByRole("option", { name: "Write" })).not.toBeInTheDocument()
  })

  it("says so when the limit of active tokens is reached", async () => {
    open("operator", "/account", {
      "GET /api/tokens": () => json(200, { tokens: [] }),
      "POST /api/tokens": () => json(409, { error: "too many active tokens" }),
    })
    const user = userEvent.setup()
    await user.type(await screen.findByLabelText("Token name"), "x")
    await user.click(screen.getByRole("button", { name: "Create token" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("20 active tokens")
  })

  it("revokes a token", async () => {
    const mock = open("operator", "/account", {
      "GET /api/tokens": () => json(200, { tokens: [token()] }),
      "DELETE /api/tokens/1": () => new Response(null, { status: 204 }),
    })
    await userEvent.setup().click(await screen.findByRole("button", { name: "Revoke ci" }))
    await waitFor(() =>
      expect(mock.calls.some((c) => c.method === "DELETE" && c.path === "/api/tokens/1")).toBe(
        true,
      ),
    )
  })
})

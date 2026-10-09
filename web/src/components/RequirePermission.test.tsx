import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { SessionProvider } from "@/api/session"
import { json, makeUser, meBody, mockFetch } from "@/test-utils"
import type { Role } from "@/lib/permissions"
import { RequirePermission } from "./RequirePermission"

afterEach(() => vi.unstubAllGlobals())

function renderAs(role: Role) {
  mockFetch({ "GET /api/auth/me": () => json(200, meBody(makeUser({ role }))) })
  render(
    <MemoryRouter>
      <SessionProvider>
        <RequirePermission permission="users:manage">
          <p>secret admin page</p>
        </RequirePermission>
      </SessionProvider>
    </MemoryRouter>,
  )
}

describe("RequirePermission", () => {
  it("shows the children to a role that holds the permission", async () => {
    renderAs("admin")
    expect(await screen.findByText("secret admin page")).toBeInTheDocument()
  })

  it.each<Role>(["viewer", "operator"])("hides the children from a %s", async (role) => {
    renderAs(role)
    expect(await screen.findByText("No access")).toBeInTheDocument()
    expect(screen.queryByText("secret admin page")).not.toBeInTheDocument()
  })
})

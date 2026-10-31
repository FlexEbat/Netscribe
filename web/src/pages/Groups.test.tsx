import { cleanup, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { createMemoryRouter, RouterProvider } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { setCsrfToken, type Group } from "@/api/client"
import type { Role } from "@/lib/permissions"
import { SessionProvider } from "@/api/session"
import { routes } from "@/App"
import { json, makeUser, meBody, mockFetch } from "@/test-utils"

afterEach(() => {
  cleanup()
  setCsrfToken("")
  vi.unstubAllGlobals()
})

const office: Group = {
  id: 1,
  name: "Office",
  vlan: 10,
  description: "Ground floor",
  ranges: ["192.168.1.0/24", "192.168.2.0/24"],
  deviceCount: 4,
  version: 3,
}

function open(groups: Group[], extra: Record<string, () => Response> = {}, role: Role = "admin") {
  const mock = mockFetch({
    "GET /api/auth/me": () => json(200, meBody(makeUser({ role }))),
    "GET /api/groups": () => json(200, { groups }),
    ...extra,
  })
  const router = createMemoryRouter(routes, { initialEntries: ["/groups"] })
  render(
    <SessionProvider>
      <RouterProvider router={router} />
    </SessionProvider>,
  )
  return mock
}

describe("Groups", () => {
  it("lists the groups with VLAN, ranges and the number of devices", async () => {
    open([office])
    const row = within(await screen.findByRole("table")).getAllByRole("row")[1]!
    expect(within(row).getByText("Office")).toBeInTheDocument()
    expect(within(row).getByText("Ground floor")).toBeInTheDocument()
    expect(within(row).getByText("10")).toBeInTheDocument()
    expect(within(row).getByText("192.168.1.0/24, 192.168.2.0/24")).toBeInTheDocument()
    expect(within(row).getByText("4")).toBeInTheDocument()
  })

  it("shows an empty state", async () => {
    open([])
    expect(await screen.findByText("No groups yet")).toBeInTheDocument()
  })

  it("offers a viewer no way to change anything", async () => {
    open([office], {}, "viewer")
    await screen.findByRole("table")
    expect(screen.queryByRole("button", { name: "Add group" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /Edit|Delete/ })).not.toBeInTheDocument()
  })

  it("creates a group from the form and reloads the list", async () => {
    const mock = open([], { "POST /api/groups": () => json(201, { group: office }) })
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "Add group" }))
    const dialog = await screen.findByRole("dialog")
    await user.type(within(dialog).getByLabelText("Name"), "Office")
    await user.type(within(dialog).getByLabelText("VLAN (optional)"), "10")
    await user.type(within(dialog).getByLabelText("Ranges"), "192.168.1.0/24, 10.0.0.0/8")
    await user.click(within(dialog).getByRole("button", { name: "Add group" }))

    await waitFor(() => expect(mock.calls.filter((c) => c.method === "POST")).toHaveLength(1))
    expect(JSON.parse(String(mock.calls.find((c) => c.method === "POST")!.init.body))).toEqual({
      name: "Office",
      description: "",
      vlan: 10,
      ranges: ["192.168.1.0/24", "10.0.0.0/8"],
    })
    await waitFor(() =>
      expect(mock.calls.filter((c) => c.path === "/api/groups" && c.method === "GET")).toHaveLength(
        2,
      ),
    )
  })

  it("shows why a group could not be saved and keeps the form", async () => {
    open([], {
      "POST /api/groups": () => json(409, { error: "a group with this name already exists" }),
    })
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "Add group" }))
    const dialog = await screen.findByRole("dialog")
    await user.type(within(dialog).getByLabelText("Name"), "Office")
    await user.click(within(dialog).getByRole("button", { name: "Add group" }))
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("already exists")
    expect(within(dialog).getByLabelText("Name")).toHaveValue("Office")
  })

  it("edits with the version it loaded and removes a VLAN that was cleared", async () => {
    const mock = open([office], {
      "PATCH /api/groups/1": () => json(200, { group: { ...office, vlan: null } }),
    })
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "Edit Office" }))
    const dialog = await screen.findByRole("dialog")
    await user.clear(within(dialog).getByLabelText("VLAN (optional)"))
    await user.click(within(dialog).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mock.calls.some((c) => c.method === "PATCH")).toBe(true))
    const body = JSON.parse(String(mock.calls.find((c) => c.method === "PATCH")!.init.body))
    expect(body).toMatchObject({ name: "Office", vlan: 0, version: 3, ranges: office.ranges })
  })

  it("deletes after a confirmation", async () => {
    const mock = open([office], { "DELETE /api/groups/1": () => json(204) })
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "Delete Office" }))
    const dialog = await screen.findByRole("alertdialog")
    expect(mock.calls.some((c) => c.method === "DELETE")).toBe(false)
    await user.click(within(dialog).getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(mock.calls.some((c) => c.method === "DELETE")).toBe(true))
  })
})

import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { createMemoryRouter, RouterProvider } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { setCsrfToken, type Device } from "@/api/client"
import { SessionProvider } from "@/api/session"
import { routes } from "@/App"
import { json, makeUser, meBody, mockFetch } from "@/test-utils"

afterEach(() => {
  setCsrfToken("")
  vi.unstubAllGlobals()
})

function device(overrides: Partial<Device>): Device {
  return {
    id: 1,
    mac: "aa:bb:cc:dd:ee:01",
    ip: "192.168.1.2",
    hostname: "gateway",
    vendor: "",
    kind: "router",
    description: "",
    source: "arp",
    online: true,
    manual: false,
    label: "",
    notes: "",
    kindLocked: false,
    version: 1,
    firstSeenAt: "2026-10-09T12:00:00.000Z",
    lastSeenAt: "2026-10-09T12:00:00.000Z",
    x: null,
    y: null,
    ...overrides,
  }
}

function open(devices: (path: string) => Response) {
  const mock = mockFetch({
    "GET /api/auth/me": () => json(200, meBody(makeUser())),
  })
  const base = globalThis.fetch as unknown as (
    i: RequestInfo | URL,
    init?: RequestInit,
  ) => Promise<Response>
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path.startsWith("/api/devices")) {
      mock.calls.push({ method: "GET", path, init: init ?? {} })
      return devices(path)
    }
    return base(input, init)
  })
  const router = createMemoryRouter(routes, { initialEntries: ["/inventory"] })
  render(
    <SessionProvider>
      <RouterProvider router={router} />
    </SessionProvider>,
  )
  return mock
}

describe("Inventory", () => {
  it("lists devices in numeric IP order with their status", async () => {
    open(() =>
      json(200, [
        device({ id: 1, ip: "192.168.1.10", hostname: "nas" }),
        device({ id: 2, ip: "192.168.1.9", hostname: "printer", online: false }),
      ]),
    )
    const table = await screen.findByRole("table")
    const rows = within(table).getAllByRole("row").slice(1)
    expect(rows[0]).toHaveTextContent("192.168.1.9")
    expect(rows[1]).toHaveTextContent("192.168.1.10")
    expect(rows[0]).toHaveTextContent("Offline")
    expect(rows[1]).toHaveTextContent("Online")
  })

  it("asks the server to search and filter", async () => {
    const mock = open(() => json(200, [device({})]))
    await screen.findByRole("table")
    const user = userEvent.setup()
    await user.selectOptions(screen.getByLabelText("Filter by status"), "offline")
    await waitFor(() => expect(mock.calls.some((c) => c.path.includes("online=false"))).toBe(true))
    await user.type(screen.getByLabelText("Search devices"), "роутер")
    await waitFor(() =>
      expect(
        mock.calls.some((c) => c.path.includes("q=%D1%80%D0%BE%D1%83%D1%82%D0%B5%D1%80")),
      ).toBe(true),
    )
  })

  it("shows an empty state, and a different one when a filter hides everything", async () => {
    open(() => json(200, []))
    expect(await screen.findByText("No devices yet")).toBeInTheDocument()
    await userEvent.setup().type(screen.getByLabelText("Search devices"), "x")
    expect(await screen.findByText("No matches")).toBeInTheDocument()
  })

  it("shows a generic message when the request fails", async () => {
    open(() => json(500, { error: "database is locked: /var/lib/netscribe/netscribe.db" }))
    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("The devices could not be loaded.")
    expect(alert).not.toHaveTextContent("database")
  })
})

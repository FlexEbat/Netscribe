import { render, screen, within } from "@testing-library/react"
import { createMemoryRouter, RouterProvider } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { setCsrfToken } from "@/api/client"
import type { Device } from "@/api/devices"
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
    firstSeenAt: "2026-10-09T12:00:00.000Z",
    lastSeenAt: "2026-10-09T12:00:00.000Z",
    ...overrides,
  }
}

function open(devices: () => Response) {
  mockFetch({
    "GET /api/auth/me": () => json(200, meBody(makeUser())),
    "GET /api/devices": devices,
  })
  const router = createMemoryRouter(routes, { initialEntries: ["/"] })
  render(
    <SessionProvider>
      <RouterProvider router={router} />
    </SessionProvider>,
  )
}

describe("Topology", () => {
  it("lists the devices with their status", async () => {
    open(() =>
      json(200, {
        devices: [
          device({}),
          device({
            id: 2,
            ip: "192.168.1.10",
            mac: "",
            hostname: "",
            kind: "unknown",
            online: false,
          }),
        ],
      }),
    )
    const table = await screen.findByRole("table")
    expect(within(table).getByText("gateway")).toBeInTheDocument()
    expect(within(table).getByText("Online")).toBeInTheDocument()
    expect(within(table).getByText("Offline")).toBeInTheDocument()
    expect(screen.getByText("Devices (2)")).toBeInTheDocument()
    // A device without a MAC or a hostname shows a dash instead of an empty cell.
    expect(within(table).getAllByText("–")).toHaveLength(2)
  })

  it("explains the empty state before the first scan", async () => {
    open(() => json(200, { devices: [] }))
    expect(await screen.findByText("No scans yet")).toBeInTheDocument()
  })

  it("shows a generic message when the request fails", async () => {
    open(() => json(500, { error: "database is locked: /var/lib/netscribe/netscribe.db" }))
    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("The devices could not be loaded.")
    expect(alert).not.toHaveTextContent("database")
  })
})

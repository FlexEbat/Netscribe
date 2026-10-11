import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { createMemoryRouter, RouterProvider } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { setCsrfToken, type Device, type Scan } from "@/api/client"
import type { Role } from "@/lib/permissions"
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

function scan(overrides: Partial<Scan> = {}): Scan {
  return {
    id: 1,
    target: "192.168.1.0/24",
    status: "done",
    startedAt: "2026-10-09T12:00:00.000Z",
    finishedAt: "2026-10-09T12:00:05.000Z",
    error: "",
    deviceCount: 2,
    linkCount: 0,
    containerCount: 0,
    ...overrides,
  }
}

class FakeEventSource {
  static instances: FakeEventSource[] = []
  closed = false
  private listeners = new Map<string, ((e: MessageEvent<string>) => void)[]>()
  constructor(readonly url: string) {
    FakeEventSource.instances.push(this)
  }
  addEventListener(name: string, fn: (e: MessageEvent<string>) => void) {
    this.listeners.set(name, [...(this.listeners.get(name) ?? []), fn])
  }
  close() {
    this.closed = true
  }
  emit(name: string, data: unknown) {
    for (const fn of this.listeners.get(name) ?? []) {
      fn(new MessageEvent(name, { data: JSON.stringify(data) }))
    }
  }
}

interface OpenOptions {
  role?: Role
  scans?: Scan[]
  startResponse?: () => Response
}

function open(devices: (path: string) => Response, options: OpenOptions = {}) {
  FakeEventSource.instances = []
  vi.stubGlobal("EventSource", FakeEventSource)
  const mock = mockFetch({
    "GET /api/auth/me": () => json(200, meBody(makeUser({ role: options.role ?? "admin" }))),
    "GET /api/scans": () => json(200, { scans: options.scans ?? [] }),
    "POST /api/scans": options.startResponse ?? (() => json(202, scan({ status: "running" }))),
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

function stream(): FakeEventSource {
  const source = FakeEventSource.instances.at(-1)
  if (!source) throw new Error("the page did not open an event stream")
  return source
}

describe("Inventory", () => {
  it("lists devices in numeric IP order with their status", async () => {
    open(() =>
      json(200, {
        devices: [
          device({ id: 1, ip: "192.168.1.10", hostname: "nas" }),
          device({ id: 2, ip: "192.168.1.9", hostname: "printer", online: false }),
        ],
      }),
    )
    const table = await screen.findByRole("table")
    const rows = within(table).getAllByRole("row").slice(1)
    expect(rows[0]).toHaveTextContent("192.168.1.9")
    expect(rows[1]).toHaveTextContent("192.168.1.10")
    expect(rows[0]).toHaveTextContent("Offline")
    expect(rows[1]).toHaveTextContent("Online")
  })

  it("asks the server to search and filter", async () => {
    const mock = open(() => json(200, { devices: [device({})] }))
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
    open(() => json(200, { devices: [] }))
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

  it("offers a first scan to a role that may run scans, and reports a scan in progress", async () => {
    open(() => json(200, { devices: [] }), { role: "operator" })
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "Run first scan" }))
    expect(await screen.findByText("Scan in progress")).toBeInTheDocument()
  })

  it("does not offer scans to a viewer", async () => {
    open(() => json(200, { devices: [] }), { role: "viewer" })
    expect(await screen.findByText("No devices yet")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /scan/i })).not.toBeInTheDocument()
  })

  it("says so when a scan is already running", async () => {
    open(() => json(200, { devices: [] }), {
      scans: [scan()],
      startResponse: () => json(409, { error: "a scan is already running" }),
    })
    await userEvent.setup().click(await screen.findByRole("button", { name: "Run scan" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("A scan is already running.")
  })

  it("shows progress from the event stream and reloads the devices when the topology changes", async () => {
    const mock = open(() => json(200, { devices: [device({})] }), { scans: [scan()] })
    await screen.findByRole("table")
    expect(screen.getByText(/Last scan: 2 devices/)).toBeInTheDocument()
    const loads = () => mock.calls.filter((c) => c.path.startsWith("/api/devices")).length
    const before = loads()

    act(() => stream().emit("scan.started", scan({ id: 2, status: "running" })))
    act(() => stream().emit("scan.progress", { scanId: 2, stage: "dns", done: 2, total: 3 }))
    expect(screen.getByRole("progressbar", { name: "Scan progress" })).toHaveAttribute(
      "aria-valuenow",
      "2",
    )

    act(() => stream().emit("scan.finished", scan({ id: 2, deviceCount: 5 })))
    act(() => stream().emit("topology.changed", {}))
    expect(await screen.findByText(/Last scan: 5 devices/)).toBeInTheDocument()
    await waitFor(() => expect(loads()).toBe(before + 1))
  })

  it("closes the event stream when the page is left", async () => {
    open(() => json(200, { devices: [] }))
    await screen.findByText("No devices yet")
    const source = stream()
    expect(source.closed).toBe(false)
    cleanup()
    expect(source.closed).toBe(true)
  })
})

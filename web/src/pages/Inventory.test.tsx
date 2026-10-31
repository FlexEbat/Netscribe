import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { createMemoryRouter, RouterProvider } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { setCsrfToken, type Device, type NmapState, type Scan } from "@/api/client"
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
    osGuess: "",
    kindLocked: false,
    trusted: true,
    groupId: null,
    group: null,
    groupAuto: false,
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
  /** The address to open, with its query. */
  entry?: string
  role?: Role
  scans?: Scan[]
  startResponse?: () => Response
  nmap?: NmapState
}

function open(devices: (path: string, init?: RequestInit) => Response, options: OpenOptions = {}) {
  FakeEventSource.instances = []
  vi.stubGlobal("EventSource", FakeEventSource)
  const mock = mockFetch({
    "GET /api/auth/me": () => json(200, meBody(makeUser({ role: options.role ?? "admin" }))),
    "GET /api/scans": () => json(200, { scans: options.scans ?? [] }),
    "POST /api/scans": options.startResponse ?? (() => json(202, scan({ status: "running" }))),
    "GET /api/nmap": () =>
      options.nmap ? json(200, options.nmap) : json(404, { error: "not found" }),
  })
  const base = globalThis.fetch as unknown as (
    i: RequestInfo | URL,
    init?: RequestInit,
  ) => Promise<Response>
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path.startsWith("/api/devices")) {
      mock.calls.push({ method: (init?.method ?? "GET").toUpperCase(), path, init: init ?? {} })
      return devices(path, init)
    }
    return base(input, init)
  })
  const router = createMemoryRouter(routes, { initialEntries: [options.entry ?? "/inventory"] })
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

describe("Inventory keyboard and layout", () => {
  it("focuses the search field when / is pressed", async () => {
    open(() => json(200, { devices: [device({})] }))
    await screen.findByText("gateway")
    await userEvent.keyboard("/")
    expect(screen.getByRole("searchbox", { name: "Search devices" })).toHaveFocus()
  })

  it("types / into the search field instead of refocusing it", async () => {
    open(() => json(200, { devices: [device({})] }))
    await screen.findByText("gateway")
    const field = screen.getByRole("searchbox", { name: "Search devices" })
    await userEvent.click(field)
    await userEvent.keyboard("a/b")
    expect(field).toHaveValue("a/b")
  })

  it("does not steal / from the status filter", async () => {
    open(() => json(200, { devices: [device({})] }))
    await screen.findByText("gateway")
    const filter = screen.getByRole("combobox", { name: "Filter by status" })
    filter.focus()
    await userEvent.keyboard("/")
    expect(filter).toHaveFocus()
  })

  it("opens the navigation in a sheet behind the menu button", async () => {
    open(() => json(200, { devices: [device({})] }))
    await screen.findByText("gateway")
    await userEvent.click(screen.getByRole("button", { name: "Open navigation" }))
    const sheet = await screen.findByRole("dialog", { name: "Navigation" })
    expect(within(sheet).getByRole("link", { name: "Services" })).toBeInTheDocument()
  })
})

describe("Inventory nmap option", () => {
  const nmap = { available: true, enabled: false, reason: "" }
  const startCalls = (mock: ReturnType<typeof open>) =>
    mock.calls.filter((c) => c.method === "POST" && c.path === "/api/scans")

  it("starts a plain scan unless the box is ticked", async () => {
    const mock = open(() => json(200, { devices: [] }), { nmap })
    await userEvent.setup().click(await screen.findByRole("button", { name: "Run scan" }))
    await screen.findByText("Scan in progress")
    expect(startCalls(mock)).toHaveLength(1)
    expect(startCalls(mock)[0]!.init.body).toBeUndefined()
  })

  it("asks for nmap when the box is ticked", async () => {
    const mock = open(() => json(200, { devices: [] }), { nmap })
    const user = userEvent.setup()
    await user.click(
      await screen.findByRole("checkbox", { name: "Identify unknown devices with Nmap" }),
    )
    await user.click(screen.getByRole("button", { name: "Run scan" }))
    await screen.findByText("Scan in progress")
    expect(JSON.parse(String(startCalls(mock)[0]!.init.body))).toEqual({ nmap: true })
  })

  it("shows no box when the server has no nmap", async () => {
    open(() => json(200, { devices: [] }), { nmap: { ...nmap, available: false } })
    await screen.findByRole("button", { name: "Run scan" })
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument()
  })

  it("does not ask for nmap state when the person cannot run scans", async () => {
    const mock = open(() => json(200, { devices: [device({})] }), { nmap, role: "viewer" })
    await screen.findByRole("table")
    expect(mock.calls.some((c) => c.path === "/api/nmap")).toBe(false)
  })
})

describe("Inventory trust", () => {
  const unknown = device({ id: 1, hostname: "cam1", ip: "192.168.1.9", trusted: false, version: 4 })
  const known = device({ id: 2, hostname: "nas", ip: "192.168.1.10", trusted: true })

  it("asks the server for the unknown devices only", async () => {
    const mock = open(() => json(200, { devices: [unknown, known] }))
    await screen.findByRole("table")
    await userEvent.setup().selectOptions(screen.getByLabelText("Filter by trust"), "unknown")
    await waitFor(() => expect(mock.calls.some((c) => c.path.includes("trusted=false"))).toBe(true))
  })

  it("marks an unknown device and says nothing about a trusted one", async () => {
    open(() => json(200, { devices: [unknown, known] }))
    const rows = within(await screen.findByRole("table")).getAllByRole("row")
    expect(within(rows[1]!).getByText("Unknown")).toBeInTheDocument()
    expect(within(rows[1]!).getByRole("button", { name: "Trust cam1" })).toBeInTheDocument()
    expect(within(rows[2]!).getByText("Trusted")).toBeInTheDocument()
    expect(within(rows[2]!).queryByRole("button", { name: /Trust/ })).not.toBeInTheDocument()
  })

  it("trusts a device and loads the list again", async () => {
    const seen: string[] = []
    open((path) => {
      seen.push(path)
      return path === "/api/devices/1"
        ? json(200, { device: { ...unknown, trusted: true } })
        : json(200, { devices: [unknown] })
    })
    await userEvent.setup().click(await screen.findByRole("button", { name: "Trust cam1" }))
    await waitFor(() => expect(seen).toContain("/api/devices/1"))
    await waitFor(() => expect(seen.filter((p) => p === "/api/devices")).toHaveLength(2))
  })

  it("offers a viewer the badge but not the button", async () => {
    open(() => json(200, { devices: [unknown] }), { role: "viewer" })
    expect(await screen.findByText("Unknown")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Trust cam1" })).not.toBeInTheDocument()
  })

  it("starts with the filters that the link of the overview carries", async () => {
    const mock = open(() => json(200, { devices: [unknown] }), {
      entry: "/inventory?status=offline&trust=unknown",
    })
    await screen.findByRole("table")
    const list = mock.calls.filter((c) => c.path.startsWith("/api/devices"))[0]!
    expect(list.path).toContain("online=false")
    expect(list.path).toContain("trusted=false")
    expect(screen.getByLabelText("Filter by trust")).toHaveValue("unknown")
  })

  it("ignores a filter value it does not know", async () => {
    const mock = open(() => json(200, { devices: [unknown] }), {
      entry: "/inventory?status=sideways&trust=maybe",
    })
    await screen.findByRole("table")
    const list = mock.calls.filter((c) => c.path.startsWith("/api/devices"))[0]!
    expect(list.path).not.toContain("online=")
    expect(list.path).not.toContain("trusted=")
  })
})

describe("Inventory groups", () => {
  const office = {
    id: 1,
    name: "Office",
    vlan: 10,
    description: "",
    ranges: ["192.168.1.0/24"],
    deviceCount: 2,
    version: 1,
  }
  const lab = {
    id: 2,
    name: "Lab",
    vlan: null,
    description: "",
    ranges: [],
    deviceCount: 1,
    version: 1,
  }
  const inRange = device({
    id: 1,
    hostname: "cam1",
    ip: "192.168.1.9",
    group: { id: 1, name: "Office", vlan: 10 },
    groupAuto: true,
  })
  const chosen = device({
    id: 2,
    hostname: "nas",
    ip: "192.168.1.10",
    groupId: 2,
    group: { id: 2, name: "Lab", vlan: null },
  })
  const loose = device({ id: 3, hostname: "tv", ip: "10.0.0.5" })
  const savedFilters = [
    {
      id: 1,
      page: "inventory",
      name: "Cameras",
      query: "q=cam",
      createdAt: "2026-10-08T12:00:00Z",
    },
  ]

  function openWithGroups(
    devices: (path: string, init?: RequestInit) => Response,
    options: { role?: Role; entry?: string } = {},
  ) {
    const mock = open(devices, options)
    const base = globalThis.fetch as unknown as (
      i: RequestInfo | URL,
      init?: RequestInit,
    ) => Promise<Response>
    vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input).startsWith("/api/filters")) {
        return json(200, { filters: savedFilters })
      }
      if (String(input) === "/api/groups") {
        mock.calls.push({ method: "GET", path: "/api/groups", init: init ?? {} })
        return json(200, { groups: [office, lab] })
      }
      return base(input, init)
    })
    return mock
  }

  it("shows the group of every device and says when it comes from the range", async () => {
    openWithGroups(() => json(200, { devices: [inRange, chosen, loose] }))
    const table = within(await screen.findByRole("table"))
    const row = (name: string) =>
      table.getAllByRole("row").find((r) => within(r).queryByText(name))!
    expect(within(row("cam1")).getByText("Office")).toBeInTheDocument()
    expect(within(row("cam1")).getByText("(range)")).toBeInTheDocument()
    expect(within(row("nas")).getByText("Lab")).toBeInTheDocument()
    expect(within(row("nas")).queryByText("(range)")).not.toBeInTheDocument()
    expect(within(row("tv")).queryByText("Office")).not.toBeInTheDocument()
    expect(within(row("tv")).queryByText("Lab")).not.toBeInTheDocument()
  })

  it("asks the server for the devices of one group, or of none", async () => {
    const mock = openWithGroups(() => json(200, { devices: [inRange] }))
    await screen.findByRole("table")
    const select = screen.getByLabelText("Filter by group")
    await waitFor(() =>
      expect(within(select).getByRole("option", { name: "Lab" })).toBeInTheDocument(),
    )
    await userEvent.setup().selectOptions(select, "Lab")
    await waitFor(() => expect(mock.calls.some((c) => c.path.includes("group=2"))).toBe(true))
    await userEvent.setup().selectOptions(select, "No group")
    await waitFor(() => expect(mock.calls.some((c) => c.path.includes("group=none"))).toBe(true))
  })

  it("starts with the group of the address", async () => {
    const mock = openWithGroups(() => json(200, { devices: [chosen] }), {
      entry: "/inventory?group=2",
    })
    await screen.findByRole("table")
    expect(mock.calls.find((c) => c.path.startsWith("/api/devices"))!.path).toContain("group=2")
  })

  it("changes the selected devices together and only them", async () => {
    const posted: unknown[] = []
    openWithGroups((path, init) => {
      if (path === "/api/devices/bulk") {
        posted.push(JSON.parse(String(init?.body)))
        return json(200, { updated: 2 })
      }
      return json(200, { devices: [inRange, chosen, loose] })
    })
    const user = userEvent.setup()
    await screen.findByRole("table")
    await user.click(screen.getByRole("checkbox", { name: "Select cam1" }))
    await user.click(screen.getByRole("checkbox", { name: "Select tv" }))
    expect(screen.getByText("2 devices selected")).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "Mark as trusted" }))
    await waitFor(() => expect(posted).toHaveLength(1))
    expect(posted[0]).toEqual({ ids: [1, 3], trusted: true })
    await waitFor(() => expect(screen.queryByText("2 devices selected")).not.toBeInTheDocument())
  })

  it("sets a group for the selection and shows why a change failed", async () => {
    const posted: unknown[] = []
    openWithGroups((path, init) => {
      if (path === "/api/devices/bulk") {
        posted.push(JSON.parse(String(init?.body)))
        return json(404, { error: "a listed device does not exist, nothing was changed" })
      }
      return json(200, { devices: [inRange] })
    })
    const user = userEvent.setup()
    await user.click(await screen.findByRole("checkbox", { name: "Select all devices" }))
    const set = screen.getByLabelText("Set the group of the selection")
    await waitFor(() =>
      expect(within(set).getByRole("option", { name: "Lab" })).toBeInTheDocument(),
    )
    await user.selectOptions(set, "Lab")
    expect(await screen.findByRole("alert")).toHaveTextContent("nothing was changed")
    expect(posted[0]).toEqual({ ids: [1], groupId: 2 })
    expect(screen.getByText("1 device selected")).toBeInTheDocument()
  })

  it("offers a viewer neither the checkboxes nor the import", async () => {
    openWithGroups(() => json(200, { devices: [inRange] }), { role: "viewer" })
    await screen.findByRole("table")
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Import CSV" })).not.toBeInTheDocument()
    expect(within(screen.getByRole("table")).getByText("Office")).toBeInTheDocument()
  })

  it("applies a saved filter to every field and clears what it does not name", async () => {
    const mock = openWithGroups(() => json(200, { devices: [inRange] }), {
      entry: "/inventory?status=offline&trust=unknown&group=2",
    })
    const user = userEvent.setup()
    await screen.findByRole("table")
    expect(screen.getByLabelText("Filter by trust")).toHaveValue("unknown")

    await user.click(screen.getByRole("button", { name: "Saved filters" }))
    await user.click(await screen.findByRole("button", { name: "Apply Cameras" }))

    expect(screen.getByLabelText("Search devices")).toHaveValue("cam")
    expect(screen.getByLabelText("Filter by trust")).toHaveValue("all")
    expect(screen.getByLabelText("Filter by group")).toHaveValue("all")
    await waitFor(() => {
      const last = mock.calls.filter((c) => c.path.startsWith("/api/devices")).at(-1)!
      expect(last.path).toContain("q=cam")
      expect(last.path).not.toContain("trusted=")
      expect(last.path).not.toContain("group=")
      expect(last.path).not.toContain("online=")
    })
  })
})

import { act, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { createMemoryRouter, RouterProvider } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { setCsrfToken, type Container, type Device, type Topology } from "@/api/client"
import { SessionProvider } from "@/api/session"
import { routes } from "@/App"
import { json, makeUser, meBody, mockFetch } from "@/test-utils"

// React Flow needs a layout engine that jsdom does not have, so the canvas is replaced
// by buttons that call the same props the real one calls.
vi.mock("@/components/TopologyCanvas", () => ({
  TopologyCanvas: (props: {
    topology: Topology
    readOnly?: boolean
    onMove: (id: number, x: number, y: number) => void
    onSelect: (id: number | null) => void
    onSelectContainer?: (id: string | null) => void
  }) => (
    <div data-testid="canvas" data-readonly={String(Boolean(props.readOnly))}>
      {props.topology.devices.map((d) => (
        <button key={d.id} onClick={() => props.onSelect(d.id)}>
          node {d.id}
        </button>
      ))}
      {props.topology.containers.map((c) => (
        <button key={c.id} onClick={() => props.onSelectContainer?.(c.id)}>
          container {c.name}
        </button>
      ))}
      <button onClick={() => props.onMove(1, 12.5, -3)}>drag node 1</button>
      <button onClick={() => props.onSelect(null)}>click background</button>
    </div>
  ),
}))

afterEach(() => {
  setCsrfToken("")
  vi.unstubAllGlobals()
})

function device(id: number, overrides: Partial<Device> = {}): Device {
  return {
    id,
    mac: `aa:bb:cc:dd:ee:0${id}`,
    ip: `192.168.1.${id}`,
    hostname: `host-${id}`,
    vendor: "Acme",
    kind: "host",
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
    x: id * 10,
    y: id * 10,
    ...overrides,
  }
}

const topology: Topology = { devices: [device(1), device(2)], links: [], containers: [] }

class FakeEventSource {
  static last: FakeEventSource | null = null
  private listeners = new Map<string, (e: MessageEvent<string>) => void>()
  constructor() {
    FakeEventSource.last = this
  }
  addEventListener(name: string, fn: (e: MessageEvent<string>) => void) {
    this.listeners.set(name, fn)
  }
  close() {}
  emit(name: string) {
    this.listeners.get(name)?.(new MessageEvent(name, { data: "{}" }))
  }
}

interface OpenOptions {
  role?: "viewer" | "operator" | "admin"
  topology?: () => Response
  entry?: string
  move?: () => Response
}

function open(options: OpenOptions = {}) {
  vi.stubGlobal("EventSource", FakeEventSource)
  const mock = mockFetch({
    "GET /api/auth/me": () => json(200, meBody(makeUser({ role: options.role ?? "admin" }))),
    "GET /api/topology": options.topology ?? (() => json(200, topology)),
    "GET /api/devices/2": () =>
      json(200, { device: device(2, { label: "Print room", description: "Second floor" }) }),
    "PUT /api/devices/1/position": options.move ?? (() => new Response(null, { status: 204 })),
  })
  const router = createMemoryRouter(routes, { initialEntries: [options.entry ?? "/"] })
  render(
    <SessionProvider>
      <RouterProvider router={router} />
    </SessionProvider>,
  )
  return { mock, router }
}

describe("Topology page", () => {
  it("draws the devices on the canvas", async () => {
    open()
    expect(await screen.findByTestId("canvas")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "node 2" })).toBeInTheDocument()
  })

  it("shows the empty state before the first scan", async () => {
    open({ topology: () => json(200, { devices: [], links: [], containers: [] }) })
    expect(await screen.findByText("No scans yet")).toBeInTheDocument()
  })

  it("shows a generic message when the topology cannot be loaded", async () => {
    open({ topology: () => json(500, { error: "database is locked: /var/lib/netscribe/x.db" }) })
    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("The topology could not be loaded.")
    expect(alert).not.toHaveTextContent("database")
  })

  it("opens the panel on a click, shows the device and closes it with Escape", async () => {
    const { router } = open()
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "node 2" }))
    expect(await screen.findByRole("dialog")).toBeInTheDocument()
    expect(await screen.findByText("Print room")).toBeInTheDocument()
    expect(screen.getByText("Second floor")).toBeInTheDocument()
    expect(router.state.location.search).toBe("?device=2")

    await user.keyboard("{Escape}")
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
    expect(router.state.location.search).toBe("")
  })

  it("closes the panel with its close button", async () => {
    open({ entry: "/?device=2" })
    await screen.findByText("Print room")
    await userEvent.setup().click(screen.getByRole("button", { name: "Close" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
  })

  it("opens the panel on load for /?device=<id>", async () => {
    open({ entry: "/?device=2" })
    expect(await screen.findByText("Print room")).toBeInTheDocument()
  })

  it("ignores a device parameter that is not a number", async () => {
    open({ entry: "/?device=abc" })
    await screen.findByTestId("canvas")
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
  })

  it("says so when the device in the panel does not exist", async () => {
    open({ entry: "/?device=999" })
    expect(await screen.findByText("The device could not be loaded.")).toBeInTheDocument()
  })

  it("saves the position of a dragged node", async () => {
    const { mock } = open()
    await userEvent.setup().click(await screen.findByRole("button", { name: "drag node 1" }))
    await waitFor(() => {
      const put = mock.calls.find((c) => c.method === "PUT")
      expect(put?.path).toBe("/api/devices/1/position")
      expect(JSON.parse(String(put?.init.body))).toEqual({ x: 12.5, y: -3 })
    })
  })

  it("tells the user when a position could not be saved", async () => {
    open({ move: () => json(500, { error: "disk full" }) })
    await userEvent.setup().click(await screen.findByRole("button", { name: "drag node 1" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("The position could not be saved.")
  })

  it("does not let a viewer drag nodes", async () => {
    open({ role: "viewer" })
    expect((await screen.findByTestId("canvas")).dataset.readonly).toBe("true")
  })

  it("lets an operator drag nodes", async () => {
    open({ role: "operator" })
    expect((await screen.findByTestId("canvas")).dataset.readonly).toBe("false")
  })

  it("reloads the topology when it changes", async () => {
    const { mock } = open()
    await screen.findByTestId("canvas")
    const loads = () => mock.calls.filter((c) => c.path === "/api/topology").length
    const before = loads()
    act(() => FakeEventSource.last?.emit("topology.changed"))
    await waitFor(() => expect(loads()).toBe(before + 1))
  })
  it("opens the panel of a container with everything the daemon told", async () => {
    const web: Container = {
      id: "abc123",
      deviceId: 2,
      name: "web",
      image: "nginx:1.27",
      state: "running",
      composeProject: "shop",
      ports: [{ hostIp: "0.0.0.0", hostPort: 8080, containerPort: 80, proto: "tcp" }],
      networks: [{ name: "shop_default", ip: "172.18.0.3" }],
      firstSeenAt: "2026-10-09T12:00:00.000Z",
      lastSeenAt: "2026-10-09T12:05:00.000Z",
    }
    const user = userEvent.setup()
    const { router } = open({
      topology: () => json(200, { ...topology, containers: [web] }),
    })
    await user.click(await screen.findByRole("button", { name: "container web" }))

    const panel = await screen.findByRole("dialog")
    expect(within(panel).getByText("web")).toBeInTheDocument()
    expect(within(panel).getByText("nginx:1.27")).toBeInTheDocument()
    expect(within(panel).getByText("running")).toBeInTheDocument()
    expect(within(panel).getByText("shop")).toBeInTheDocument()
    expect(within(panel).getByText("8080->80/tcp")).toBeInTheDocument()
    expect(within(panel).getByText("shop_default")).toBeInTheDocument()
    expect(within(panel).getByText("172.18.0.3")).toBeInTheDocument()
    expect(router.state.location.search).toBe("?container=abc123")
    expect(screen.queryByRole("button", { name: /^Edit$/ })).not.toBeInTheDocument()

    await user.keyboard("{Escape}")
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
    expect(router.state.location.search).toBe("")
  })

  it("closes the panel of a container that is removed", async () => {
    const web: Container = {
      id: "abc123",
      deviceId: 2,
      name: "web",
      image: "nginx:1.27",
      state: "running",
      composeProject: "",
      ports: [],
      networks: [],
      firstSeenAt: "",
      lastSeenAt: "",
    }
    let containers = [web]
    open({ topology: () => json(200, { ...topology, containers }), entry: "/?container=abc123" })
    expect(await screen.findByRole("dialog")).toBeInTheDocument()

    containers = []
    act(() => FakeEventSource.last?.emit("topology.changed"))
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
  })
  it("offers the exports to everyone who may export, and nothing to add to a viewer", async () => {
    const user = userEvent.setup()
    open({ role: "viewer" })
    await user.click(await screen.findByRole("button", { name: "Export" }))
    const items = await screen.findAllByRole("menuitem")
    expect(items.map((i) => i.getAttribute("href"))).toEqual([
      "/api/export/md",
      "/api/export/drawio",
    ])
    expect(screen.queryByRole("button", { name: /add device/i })).not.toBeInTheDocument()
  })
})

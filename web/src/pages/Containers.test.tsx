import { act, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { Container, ContainerList, Device, DockerState } from "@/api/client"
import { Containers } from "@/pages/Containers"
import { json, mockFetch } from "@/test-utils"

afterEach(() => vi.unstubAllGlobals())

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

function device(id: number, ip: string, hostname: string): Device {
  return {
    id,
    mac: `aa:bb:cc:dd:ee:0${id}`,
    ip,
    hostname,
    vendor: "",
    kind: "server",
    description: "",
    source: "local",
    online: true,
    manual: false,
    label: "",
    notes: "",
    osGuess: "",
    kindLocked: false,
    trusted: true,
    version: 1,
    firstSeenAt: "2026-10-09T12:00:00.000Z",
    lastSeenAt: "2026-10-09T12:00:00.000Z",
    x: null,
    y: null,
  }
}

function container(id: string, name: string, overrides: Partial<Container> = {}): Container {
  return {
    id,
    deviceId: 1,
    name,
    image: `${name}:1`,
    state: "running",
    composeProject: "",
    ports: [],
    networks: [],
    firstSeenAt: "2026-10-09T12:00:00.000Z",
    lastSeenAt: "2026-10-09T12:00:00.000Z",
    ...overrides,
  }
}

const up: DockerState = { enabled: true, available: true, reason: "" }

function setup(list: ContainerList | (() => ContainerList)) {
  vi.stubGlobal("EventSource", FakeEventSource)
  const mock = mockFetch({
    "GET /api/containers": () => json(200, typeof list === "function" ? list() : list),
    "GET /api/devices": () => json(200, { devices: [device(1, "192.168.1.5", "dockerhost")] }),
  })
  render(<Containers />)
  return mock
}

describe("Containers page", () => {
  it("shows name, image, state, ports, networks, project and host of every container", async () => {
    setup({
      docker: up,
      containers: [
        container("a", "web", {
          composeProject: "shop",
          ports: [
            { hostIp: "0.0.0.0", hostPort: 8080, containerPort: 80, proto: "tcp" },
            { hostIp: "::", hostPort: 8080, containerPort: 80, proto: "tcp" },
            { hostIp: "", hostPort: 0, containerPort: 443, proto: "tcp" },
          ],
          networks: [{ name: "shop_default", ip: "172.18.0.3" }],
        }),
        container("b", "db", { state: "exited" }),
      ],
    })
    const rows = within(await screen.findByRole("table")).getAllByRole("row")
    expect(rows).toHaveLength(3)
    // Ordered by host, then by name.
    expect(within(rows[1]!).getByText("db")).toBeInTheDocument()
    expect(within(rows[1]!).getByText("exited")).toBeInTheDocument()
    const web = within(rows[2]!)
    expect(web.getByText("web")).toBeInTheDocument()
    expect(web.getByText("nginx:1".replace("nginx", "web"))).toBeInTheDocument()
    expect(web.getByText("running")).toBeInTheDocument()
    expect(web.getByText("8080->80/tcp")).toBeInTheDocument()
    expect(web.getByText("443/tcp")).toBeInTheDocument()
    expect(web.getByText("shop_default")).toBeInTheDocument()
    expect(web.getByText("172.18.0.3")).toBeInTheDocument()
    expect(web.getByText("shop")).toBeInTheDocument()
    expect(web.getByText("dockerhost")).toBeInTheDocument()
    // The IPv6 twin of a published port is not listed twice.
    expect(web.getAllByText("8080->80/tcp")).toHaveLength(1)
  })

  it("filters by name, image, state or project", async () => {
    setup({
      docker: up,
      containers: [container("a", "web", { composeProject: "shop" }), container("b", "db")],
    })
    const user = userEvent.setup()
    await screen.findByRole("table")
    await user.type(screen.getByLabelText("Filter containers"), "shop")
    const rows = within(screen.getByRole("table")).getAllByRole("row")
    expect(rows).toHaveLength(2)
    expect(screen.queryByText("db")).not.toBeInTheDocument()
    await user.clear(screen.getByLabelText("Filter containers"))
    await user.type(screen.getByLabelText("Filter containers"), "nothing like this")
    expect(screen.getByText("No matches")).toBeInTheDocument()
  })

  it("explains why when the Docker socket cannot be read", async () => {
    setup({
      docker: { enabled: true, available: false, reason: "the Docker socket does not exist" },
      containers: [],
    })
    expect(await screen.findByText("Docker cannot be read")).toBeInTheDocument()
    expect(screen.getByText(/The Docker socket does not exist\./)).toBeInTheDocument()
    expect(screen.queryByRole("table")).not.toBeInTheDocument()
  })

  it("says so when Docker is turned off", async () => {
    setup({ docker: { enabled: false, available: false, reason: "" }, containers: [] })
    expect(await screen.findByText("Docker is turned off")).toBeInTheDocument()
    expect(screen.getByText(/docker\.enabled/)).toBeInTheDocument()
  })

  it("says so when the host runs no containers", async () => {
    setup({ docker: up, containers: [] })
    expect(await screen.findByText("No containers yet")).toBeInTheDocument()
  })

  it("shows an error when the list cannot be loaded", async () => {
    vi.stubGlobal("EventSource", FakeEventSource)
    mockFetch({
      "GET /api/containers": () => json(500, { error: "boom" }),
      "GET /api/devices": () => json(200, { devices: [] }),
    })
    render(<Containers />)
    expect(await screen.findByText(/could not be loaded/)).toBeInTheDocument()
  })

  it("updates when a container starts or is removed, without a reload", async () => {
    let list: ContainerList = { docker: up, containers: [container("a", "web")] }
    const mock = setup(() => list)
    await screen.findByText("web")

    list = { docker: up, containers: [container("a", "web"), container("b", "cache")] }
    act(() => FakeEventSource.last?.emit("topology.changed"))
    expect(await screen.findByText("cache")).toBeInTheDocument()

    list = { docker: up, containers: [container("b", "cache")] }
    act(() => FakeEventSource.last?.emit("topology.changed"))
    await waitFor(() => expect(screen.queryByText("web")).not.toBeInTheDocument())
    expect(mock.calls.filter((c) => c.path === "/api/containers").length).toBe(3)
  })
})

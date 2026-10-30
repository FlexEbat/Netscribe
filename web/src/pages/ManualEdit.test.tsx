import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { createMemoryRouter, RouterProvider } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { setCsrfToken, type Device, type Link, type Topology } from "@/api/client"
import { SessionProvider } from "@/api/session"
import { routes } from "@/App"
import { json, makeUser, meBody, mockFetch } from "@/test-utils"

// React Flow cannot run in jsdom, so the canvas is replaced by buttons that call the
// props the real canvas calls.
vi.mock("@/components/TopologyCanvas", () => ({
  TopologyCanvas: (props: {
    topology: Topology
    readOnly?: boolean
    onSelect: (id: number | null) => void
    onConnectNodes?: (a: number, b: number) => void
    onSelectLink?: (id: number | null) => void
  }) => (
    <div data-testid="canvas" data-readonly={String(Boolean(props.readOnly))}>
      {props.topology.devices.map((d) => (
        <button key={d.id} onClick={() => props.onSelect(d.id)}>
          node {d.id}
        </button>
      ))}
      {props.topology.links.map((l) => (
        <button key={l.id} onClick={() => props.onSelectLink?.(l.id)}>
          link {l.id}
        </button>
      ))}
      <button onClick={() => props.onConnectNodes?.(1, 2)}>connect 1 and 2</button>
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
    mac: "",
    ip: `192.168.1.${id}`,
    hostname: "",
    vendor: "",
    kind: "host",
    description: "",
    source: "manual",
    online: true,
    manual: true,
    label: `Device ${id}`,
    notes: "",
    osGuess: "",
    kindLocked: false,
    trusted: true,
    version: 3,
    firstSeenAt: "2026-10-09T12:00:00.000Z",
    lastSeenAt: "2026-10-09T12:00:00.000Z",
    x: id * 10,
    y: id * 10,
    ...overrides,
  }
}

function link(id: number, source: string): Link {
  return { id, aDeviceId: 1, aPort: "Gi0/1", bDeviceId: 2, bPort: "eth0", source, lastSeenAt: "" }
}

const topology: Topology = {
  devices: [device(1), device(2)],
  links: [link(5, "manual"), link(6, "lldp")],
  containers: [],
}

type Handler = () => Response

function open(role: "viewer" | "operator", handlers: Record<string, Handler> = {}, entry = "/") {
  vi.stubGlobal("EventSource", undefined)
  const mock = mockFetch({
    "GET /api/auth/me": () => json(200, meBody(makeUser({ role }))),
    "GET /api/topology": () => json(200, topology),
    "GET /api/devices": () => json(200, { devices: topology.devices }),
    "GET /api/scans": () => json(200, { scans: [] }),
    "GET /api/devices/2": () => json(200, { device: device(2) }),
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

function lastCall(mock: ReturnType<typeof open>, method: string, path: string) {
  const call = [...mock.calls].reverse().find((c) => c.method === method && c.path === path)
  return call ? (JSON.parse(String(call.init.body ?? "null")) as unknown) : undefined
}

describe("adding a device", () => {
  it("is offered to an operator on the canvas and in the inventory, and not to a viewer", async () => {
    open("operator")
    expect(await screen.findByRole("button", { name: "Add device" })).toBeInTheDocument()
  })

  it("is not offered to a viewer", async () => {
    open("viewer")
    await screen.findByTestId("canvas")
    expect(screen.queryByRole("button", { name: "Add device" })).not.toBeInTheDocument()
  })

  it("is offered in the inventory", async () => {
    open("operator", {}, "/inventory")
    expect(await screen.findByRole("button", { name: "Add device" })).toBeInTheDocument()
  })

  it("sends the form and reloads the topology", async () => {
    const mock = open("operator", {
      "POST /api/devices": () => json(201, { device: device(9, { label: "Patch panel" }) }),
    })
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "Add device" }))
    await user.type(await screen.findByLabelText("Name"), "Patch panel")
    await user.type(screen.getByLabelText("IP address"), "192.168.1.9")
    const before = mock.calls.filter((c) => c.path === "/api/topology").length
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Add device" }))

    await waitFor(() =>
      expect(lastCall(mock, "POST", "/api/devices")).toEqual({
        label: "Patch panel",
        hostname: "",
        notes: "",
        kind: "unknown",
        ip: "192.168.1.9",
        mac: "",
      }),
    )
    await waitFor(() =>
      expect(mock.calls.filter((c) => c.path === "/api/topology").length).toBe(before + 1),
    )
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
  })

  it("shows the message of a rejected form", async () => {
    open("operator", { "POST /api/devices": () => json(400, { error: "ip is not valid" }) })
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "Add device" }))
    await user.type(await screen.findByLabelText("IP address"), "999.1.1.1")
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Add device" }))
    expect(await within(screen.getByRole("dialog")).findByRole("alert")).toHaveTextContent(
      "ip is not valid",
    )
  })

  it("says that the address is taken", async () => {
    open("operator", {
      "POST /api/devices": () => json(409, { error: "a device with this address already exists" }),
    })
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "Add device" }))
    await user.type(await screen.findByLabelText("IP address"), "192.168.1.2")
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Add device" }))
    expect(await within(screen.getByRole("dialog")).findByRole("alert")).toHaveTextContent(
      "A device with this address already exists.",
    )
  })
})

describe("editing a device", () => {
  it("shows no edit buttons to a viewer", async () => {
    open("viewer", {}, "/?device=2")
    await screen.findByText("Device 2")
    expect(screen.queryByRole("button", { name: "Edit" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Delete" })).not.toBeInTheDocument()
  })

  it("saves the changes with the version the editor saw", async () => {
    const mock = open(
      "operator",
      {
        "PATCH /api/devices/2": () =>
          json(200, { device: device(2, { label: "Core switch", version: 4 }) }),
      },
      "/?device=2",
    )
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "Edit" }))
    const name = screen.getByLabelText("Name")
    await user.clear(name)
    await user.type(name, "Core switch")
    await user.click(screen.getByRole("button", { name: "Save" }))

    await waitFor(() =>
      expect(lastCall(mock, "PATCH", "/api/devices/2")).toEqual({
        label: "Core switch",
        notes: "",
        kind: "host",
        version: 3,
      }),
    )
    expect(await screen.findByText("Core switch")).toBeInTheDocument()
  })

  it("offers the current data after a version conflict", async () => {
    const mock = open(
      "operator",
      {
        "PATCH /api/devices/2": () =>
          json(409, { error: "the device was changed by someone else" }),
      },
      "/?device=2",
    )
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "Edit" }))
    await user.type(screen.getByLabelText("Name"), "!")
    await user.click(screen.getByRole("button", { name: "Save" }))

    expect(await screen.findByRole("alert")).toHaveTextContent("Someone else changed this device")
    const loads = () =>
      mock.calls.filter((c) => c.method === "GET" && c.path === "/api/devices/2").length
    const before = loads()
    await user.click(screen.getByRole("button", { name: "Load the current data" }))
    await waitFor(() => expect(loads()).toBe(before + 1))
  })

  it("deletes a device after a confirmation", async () => {
    const mock = open(
      "operator",
      { "DELETE /api/devices/2": () => new Response(null, { status: 204 }) },
      "/?device=2",
    )
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "Delete" }))
    expect(await screen.findByRole("alertdialog")).toHaveTextContent("Delete Device 2?")
    expect(mock.calls.some((c) => c.method === "DELETE")).toBe(false)
    await user.click(
      within(screen.getByRole("alertdialog")).getByRole("button", { name: "Delete" }),
    )
    await waitFor(() =>
      expect(mock.calls.some((c) => c.method === "DELETE" && c.path === "/api/devices/2")).toBe(
        true,
      ),
    )
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
  })

  it("does not delete when the confirmation is cancelled", async () => {
    const mock = open("operator", {}, "/?device=2")
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "Delete" }))
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Cancel" }),
    )
    expect(mock.calls.some((c) => c.method === "DELETE")).toBe(false)
  })
})

describe("links", () => {
  it("asks for the ports of a connection and creates the link", async () => {
    const mock = open("operator", {
      "POST /api/links": () => json(201, { link: link(7, "manual") }),
    })
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "connect 1 and 2" }))
    await user.type(await screen.findByLabelText("Port on Device 1"), "Gi0/2")
    await user.type(screen.getByLabelText("Port on Device 2"), "eth1")
    await user.click(screen.getByRole("button", { name: "Create link" }))
    await waitFor(() =>
      expect(lastCall(mock, "POST", "/api/links")).toEqual({
        aDeviceId: 1,
        aPort: "Gi0/2",
        bDeviceId: 2,
        bPort: "eth1",
      }),
    )
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
  })

  it("says so when the link exists already", async () => {
    open("operator", { "POST /api/links": () => json(409, { error: "this link already exists" }) })
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "connect 1 and 2" }))
    await user.click(await screen.findByRole("button", { name: "Create link" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("This link already exists.")
  })

  it("lets an operator delete a link drawn by hand", async () => {
    const mock = open("operator", {
      "DELETE /api/links/5": () => new Response(null, { status: 204 }),
    })
    const user = userEvent.setup()
    await user.click(await screen.findByRole("button", { name: "link 5" }))
    expect(screen.getByText(/drawn by hand/)).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Delete link" }))
    await waitFor(() =>
      expect(mock.calls.some((c) => c.method === "DELETE" && c.path === "/api/links/5")).toBe(true),
    )
  })

  it("offers no deletion for a link found by a scan", async () => {
    open("operator")
    await userEvent.setup().click(await screen.findByRole("button", { name: "link 6" }))
    expect(screen.getByText(/found by lldp/)).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Delete link" })).not.toBeInTheDocument()
  })

  it("does not offer link deletion to a viewer", async () => {
    open("viewer")
    await userEvent.setup().click(await screen.findByRole("button", { name: "link 5" }))
    expect(screen.queryByRole("button", { name: "Delete link" })).not.toBeInTheDocument()
  })
})

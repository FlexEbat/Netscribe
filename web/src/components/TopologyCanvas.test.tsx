import { act, render } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import type { Container, Device, Topology } from "@/api/client"
import { TopologyCanvas } from "@/components/TopologyCanvas"

// jsdom has no layout, so React Flow itself cannot run here. The stand-in keeps the
// state hooks real and records the props the canvas hands to React Flow, which are
// the callbacks this file is about.
type FlowProps = Record<string, (...args: unknown[]) => void> & {
  nodes: { id: string; type?: string }[]
  edges: unknown[]
  nodesDraggable: boolean
  nodesConnectable: boolean
}
const flow: { props: FlowProps } = { props: {} as FlowProps }

vi.mock("@xyflow/react", async (importActual) => ({
  ...(await importActual<typeof import("@xyflow/react")>()),
  ReactFlow: (props: FlowProps) => {
    flow.props = props
    return <div data-testid="flow" />
  },
  Background: () => null,
  MiniMap: () => null,
  Controls: () => null,
}))
vi.mock("@xyflow/react/dist/style.css", () => ({}))

const device = (id: number): Device => ({
  id,
  mac: "",
  ip: `10.0.0.${id}`,
  hostname: `host${id}`,
  vendor: "",
  kind: "host",
  description: "",
  source: "arp",
  online: true,
  manual: false,
  label: "",
  notes: "",
  osGuess: "",
  kindLocked: false,
  trusted: true,
  version: 1,
  firstSeenAt: "",
  lastSeenAt: "",
  x: id * 10,
  y: id * 10,
})
const box: Container = {
  id: "c1",
  deviceId: 1,
  name: "web",
  image: "nginx",
  state: "running",
  composeProject: "",
  ports: [],
  networks: [],
  firstSeenAt: "",
  lastSeenAt: "",
}
const topology: Topology = {
  devices: [device(1), device(2)],
  links: [
    {
      id: 5,
      aDeviceId: 1,
      aPort: "",
      bDeviceId: 2,
      bPort: "",
      source: "manual",
      lastSeenAt: "",
    },
  ],
  containers: [box],
}

function mount(extra: Partial<React.ComponentProps<typeof TopologyCanvas>> = {}) {
  const handlers = {
    onMove: vi.fn(),
    onSelect: vi.fn(),
    onSelectContainer: vi.fn(),
    onConnectNodes: vi.fn(),
    onSelectLink: vi.fn(),
  }
  const view = render(<TopologyCanvas topology={topology} {...handlers} {...extra} />)
  return { handlers, view }
}
const call = (name: string, ...args: unknown[]) => act(() => flow.props[name]!(...args))

beforeEach(() => {
  flow.props = {} as FlowProps
})

describe("TopologyCanvas", () => {
  it("hands devices, containers and both kinds of edges to the canvas", () => {
    mount()
    expect(flow.props.nodes.map((n) => n.id).sort()).toEqual(["1", "2", "container-c1"])
    expect(flow.props.edges).toHaveLength(2)
  })

  it("selects a device and clears the container and link selection", () => {
    const { handlers } = mount()
    call("onNodeClick", {}, { id: "2", type: "device", data: {} })
    expect(handlers.onSelectLink).toHaveBeenCalledWith(null)
    expect(handlers.onSelectContainer).toHaveBeenCalledWith(null)
    expect(handlers.onSelect).toHaveBeenCalledWith(2)
  })

  it("selects a container and clears the device selection", () => {
    const { handlers } = mount()
    call("onNodeClick", {}, { id: "container-c1", type: "container", data: { container: box } })
    expect(handlers.onSelect).toHaveBeenCalledWith(null)
    expect(handlers.onSelectContainer).toHaveBeenCalledWith("c1")
  })

  it("selects a link from its edge, and ignores an edge without one", () => {
    const { handlers } = mount()
    call("onEdgeClick", {}, { data: { linkId: 5 } })
    expect(handlers.onSelectLink).toHaveBeenCalledWith(5)
    handlers.onSelectLink.mockClear()
    call("onEdgeClick", {}, { data: undefined })
    expect(handlers.onSelectLink).not.toHaveBeenCalled()
  })

  it("clears every selection when the empty canvas is clicked", () => {
    const { handlers } = mount()
    call("onPaneClick")
    expect(handlers.onSelect).toHaveBeenCalledWith(null)
    expect(handlers.onSelectContainer).toHaveBeenCalledWith(null)
    expect(handlers.onSelectLink).toHaveBeenCalledWith(null)
  })

  it("connects two different devices and refuses a loop or a bad id", () => {
    const { handlers } = mount()
    call("onConnect", { source: "1", target: "2" })
    expect(handlers.onConnectNodes).toHaveBeenCalledWith(1, 2)
    handlers.onConnectNodes.mockClear()
    call("onConnect", { source: "1", target: "1" })
    call("onConnect", { source: "container-c1", target: "2" })
    expect(handlers.onConnectNodes).not.toHaveBeenCalled()
  })

  it("saves the position of a dragged device but not of a container", () => {
    const { handlers } = mount()
    call("onNodeDragStop", {}, { id: "2", type: "device", position: { x: 40, y: 50 } })
    expect(handlers.onMove).toHaveBeenCalledWith(2, 40, 50)
    handlers.onMove.mockClear()
    call("onNodeDragStop", {}, { id: "container-c1", type: "container", position: { x: 1, y: 2 } })
    expect(handlers.onMove).not.toHaveBeenCalled()
  })

  it("moves the containers along while their host is dragged", () => {
    mount()
    const before = flow.props.nodes.find((n) => n.id === "container-c1") as unknown as {
      position: { x: number; y: number }
    }
    const start = { ...before.position }
    call("onNodeDrag", {}, { id: "1", type: "device", position: { x: 500, y: 500 } })
    const after = flow.props.nodes.find((n) => n.id === "container-c1") as unknown as {
      position: { x: number; y: number }
    }
    expect(after.position).not.toEqual(start)
  })

  it("does not follow a dragged container", () => {
    mount()
    expect(() =>
      call("onNodeDrag", {}, { id: "container-c1", type: "container", position: { x: 1, y: 1 } }),
    ).not.toThrow()
  })

  it("lets a read-only user neither drag nor connect", () => {
    mount({ readOnly: true })
    expect(flow.props.nodesDraggable).toBe(false)
    expect(flow.props.nodesConnectable).toBe(false)
  })

  it("rebuilds the canvas when the topology changes", () => {
    const { view, handlers } = mount()
    view.rerender(
      <TopologyCanvas
        topology={{ ...topology, devices: [device(1)], links: [], containers: [] }}
        {...handlers}
      />,
    )
    expect(flow.props.nodes.map((n) => n.id)).toEqual(["1"])
    expect(flow.props.edges).toHaveLength(0)
  })
})

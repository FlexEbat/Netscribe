import { describe, expect, it } from "vitest"
import type { Container, Device, Topology } from "@/api/client"
import {
  buildNodes,
  containerPosition,
  displayName,
  followHost,
  isContainerNode,
  mergeNodes,
  toContainerEdges,
  toContainerNodes,
  toEdges,
  toNodes,
} from "./topology"

function container(id: string, deviceId: number, name: string, state = "running"): Container {
  return {
    id,
    deviceId,
    name,
    image: `${name}:1`,
    state,
    composeProject: "",
    ports: [],
    networks: [],
    firstSeenAt: "",
    lastSeenAt: "",
  }
}

function device(id: number, overrides: Partial<Device> = {}): Device {
  return {
    id,
    mac: "",
    ip: `192.168.1.${id}`,
    hostname: "",
    vendor: "",
    kind: "host",
    description: "",
    source: "arp",
    online: true,
    manual: false,
    label: "",
    notes: "",
    kindLocked: false,
    version: 1,
    firstSeenAt: "",
    lastSeenAt: "",
    x: id * 10,
    y: id * 20,
    ...overrides,
  }
}

const topology = (devices: Device[]): Topology => ({ devices, links: [], containers: [] })

describe("toNodes", () => {
  it("puts every device at its position and counts its containers", () => {
    const t: Topology = {
      devices: [device(1), device(2)],
      links: [],
      containers: [container("a", 2, "web"), container("b", 2, "db")],
    }
    const nodes = toNodes(t)
    expect(nodes.map((n) => n.id)).toEqual(["1", "2"])
    expect(nodes[0]?.position).toEqual({ x: 10, y: 20 })
    expect(nodes.map((n) => n.data.containerCount)).toEqual([0, 2])
  })
})

describe("toEdges", () => {
  it("labels an edge with both ports and skips an empty one", () => {
    const edges = toEdges([
      {
        id: 5,
        aDeviceId: 1,
        aPort: "Gi0/1",
        bDeviceId: 2,
        bPort: "eth0",
        source: "lldp",
        lastSeenAt: "",
      },
      { id: 6, aDeviceId: 2, aPort: "", bDeviceId: 3, bPort: "", source: "lldp", lastSeenAt: "" },
    ])
    expect(edges[0]).toEqual({
      id: "link-5",
      source: "1",
      target: "2",
      label: "Gi0/1 - eth0",
      data: { linkId: 5, source: "lldp" },
    })
    expect(edges[1]?.label).toBe("")
  })
})

describe("mergeNodes", () => {
  it("keeps the nodes already on screen where they are and adds the new ones", () => {
    const current = toNodes(topology([device(1), device(2)])).map((n) =>
      n.id === "1" ? { ...n, position: { x: 999, y: 888 } } : n,
    )
    const next = toNodes(topology([device(1), device(2, { online: false }), device(3)]))
    const merged = mergeNodes(current, next)
    expect(merged.map((n) => n.id)).toEqual(["1", "2", "3"])
    expect(merged[0]?.position).toEqual({ x: 999, y: 888 })
    expect(merged[1]?.data.device.online).toBe(false)
    expect(merged[2]?.position).toEqual({ x: 30, y: 60 })
  })

  it("drops a device that is gone", () => {
    const current = toNodes(topology([device(1), device(2)]))
    expect(mergeNodes(current, toNodes(topology([device(2)]))).map((n) => n.id)).toEqual(["2"])
  })
})

describe("displayName", () => {
  it("prefers the label, then the hostname, then the address", () => {
    expect(displayName({ label: "Core", hostname: "sw1", ip: "10.0.0.1", mac: "" })).toBe("Core")
    expect(displayName({ label: "", hostname: "sw1", ip: "10.0.0.1", mac: "" })).toBe("sw1")
    expect(displayName({ label: "", hostname: "", ip: "10.0.0.1", mac: "" })).toBe("10.0.0.1")
    expect(displayName({ label: "", hostname: "", ip: "", mac: "aa:bb" })).toBe("aa:bb")
  })
})

describe("containers on the canvas", () => {
  const host = { x: 100, y: 200 }

  it("stands the containers under the host, six to a row", () => {
    expect(containerPosition(host, 0)).toEqual({ x: 100, y: 310 })
    expect(containerPosition(host, 5)).toEqual({ x: 850, y: 310 })
    expect(containerPosition(host, 6)).toEqual({ x: 100, y: 386 })
    expect(containerPosition(host, 13)).toEqual({ x: 250, y: 462 })
  })

  it("orders a host's containers by name and gives each its own node", () => {
    const t: Topology = {
      devices: [device(2)],
      links: [],
      containers: [container("z", 2, "web"), container("y", 2, "db"), container("x", 2, "cache")],
    }
    const nodes = toContainerNodes(t, new Map([["2", { x: 20, y: 40 }]]))
    expect(nodes.map((n) => n.data.container.name)).toEqual(["cache", "db", "web"])
    expect(nodes.map((n) => n.id)).toEqual(["container-x", "container-y", "container-z"])
    expect(nodes[1]?.position).toEqual(containerPosition({ x: 20, y: 40 }, 1))
    expect(nodes.every((n) => n.draggable === false && n.connectable === false)).toBe(true)
    expect(nodes.every((n) => n.data.hostId === "2")).toBe(true)
  })

  it("leaves out a container whose host is not on the canvas", () => {
    const t: Topology = { devices: [], links: [], containers: [container("a", 9, "web")] }
    expect(toContainerNodes(t, new Map())).toEqual([])
  })

  it("draws an edge from the host to each container, coloured by state", () => {
    const edges = toContainerEdges([
      container("a", 2, "web", "running"),
      container("b", 2, "db", "exited"),
      container("c", 2, "job", "restarting"),
      container("d", 2, "new", "created"),
    ])
    expect(edges.map((e) => [e.source, e.target])).toEqual([
      ["2", "container-a"],
      ["2", "container-b"],
      ["2", "container-c"],
      ["2", "container-d"],
    ])
    const strokes = edges.map((e) => e.style.stroke)
    expect(new Set(strokes).size).toBe(4)
    expect(edges[0]?.data.containerId).toBe("a")
  })

  it("keeps the containers under the host where it stands on screen", () => {
    const t: Topology = {
      devices: [device(1), device(2)],
      links: [],
      containers: [container("a", 2, "web")],
    }
    const first = buildNodes(t)
    expect(first.map((n) => n.id)).toEqual(["1", "2", "container-a"])

    const dragged = first.map((n) => (n.id === "2" ? { ...n, position: { x: 500, y: 600 } } : n))
    const next = buildNodes(t, dragged)
    const web = next.find((n) => n.id === "container-a")
    expect(web?.position).toEqual(containerPosition({ x: 500, y: 600 }, 0))
  })

  it("moves the containers along while the host is dragged", () => {
    const t: Topology = {
      devices: [device(1), device(2)],
      links: [],
      containers: [container("a", 2, "web"), container("b", 1, "db")],
    }
    const moved = followHost(buildNodes(t), "2", { x: 0, y: 0 })
    expect(moved.find((n) => n.id === "container-a")?.position).toEqual(
      containerPosition({ x: 0, y: 0 }, 0),
    )
    expect(moved.find((n) => n.id === "container-b")?.position).toEqual(
      containerPosition({ x: 10, y: 20 }, 0),
    )
  })

  it("removes the node and the edge of a container that is gone", () => {
    const t: Topology = {
      devices: [device(2)],
      links: [],
      containers: [container("a", 2, "web"), container("b", 2, "db")],
    }
    const before = buildNodes(t)
    const after = buildNodes({ ...t, containers: [container("a", 2, "web")] }, before)
    expect(after.filter(isContainerNode).map((n) => n.id)).toEqual(["container-a"])
    expect(toContainerEdges(t.containers.slice(0, 1))).toHaveLength(1)
  })
})

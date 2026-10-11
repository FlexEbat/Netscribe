import { describe, expect, it } from "vitest"
import type { Device, Topology } from "@/api/client"
import { displayName, mergeNodes, toEdges, toNodes } from "./topology"

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
      containers: [
        { id: "a", deviceId: 2, name: "web", image: "nginx", state: "running" },
        { id: "b", deviceId: 2, name: "db", image: "pg", state: "running" },
      ],
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

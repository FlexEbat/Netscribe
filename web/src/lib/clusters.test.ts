import { describe, expect, it } from "vitest"
import type { Container, Device, Link } from "@/api/client"
import {
  boxColumns,
  boxSize,
  buildClusterGraph,
  buildClusters,
  clusterKeyOf,
  clusterNodeId,
  defaultBy,
  layoutClusters,
  ROW_WIDTH,
} from "@/lib/clusters"

function device(id: number, overrides: Partial<Device> = {}): Device {
  return {
    id,
    mac: "",
    ip: `192.168.1.${id}`,
    ip6: "",
    kindScore: 0,
    kindReasons: [],
    hostname: "",
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
    groupId: null,
    group: null,
    groupAuto: false,
    version: 1,
    firstSeenAt: "",
    lastSeenAt: "",
    x: 0,
    y: 0,
    ...overrides,
  }
}

const group = (id: number, name: string, vlan: number | null = null) => ({
  id,
  name,
  vlan,
  auto: false,
  color: "",
})

function link(id: number, a: number, b: number): Link {
  return {
    id,
    aDeviceId: a,
    aPort: `p${a}`,
    bDeviceId: b,
    bPort: `p${b}`,
    source: "lldp",
    lastSeenAt: "",
  }
}

function container(id: string, deviceId: number, name = id, state = "running"): Container {
  return {
    id,
    deviceId,
    name,
    image: "img",
    state,
    composeProject: "",
    ports: [],
    networks: [],
    firstSeenAt: "",
    lastSeenAt: "",
  }
}

const noop = () => {}

describe("buildClusters", () => {
  it("gathers by group, the largest first, and counts what is online", () => {
    const g1 = group(1, "Office", 10)
    const g2 = group(2, "Cameras")
    const devices = [
      device(1, { group: g2, groupId: 2 }),
      device(2, { group: g1, groupId: 1 }),
      device(3, { group: g1, groupId: 1, online: false }),
      device(4),
    ]
    const clusters = buildClusters(devices, "group")
    expect(clusters.map((c) => [c.key, c.devices.length, c.online])).toEqual([
      ["g1", 2, 1],
      ["g2", 1, 1],
      ["none", 1, 1],
    ])
    expect(clusters[0]!.vlan).toBe(10)
  })

  it("gathers by kind with the kinds counted", () => {
    const devices = [
      device(1, { kind: "router" }),
      device(2, { kind: "host" }),
      device(3, { kind: "host" }),
    ]
    const clusters = buildClusters(devices, "kind")
    expect(clusters.map((c) => c.key)).toEqual(["khost", "krouter"])
    expect(clusters[0]!.kinds).toEqual([["host", 2]])
  })

  it("puts equal sizes in the order of their names, so the view does not jump", () => {
    const devices = [
      device(1, { group: group(2, "B"), groupId: 2 }),
      device(2, { group: group(1, "A"), groupId: 1 }),
    ]
    expect(buildClusters(devices, "group").map((c) => c.label)).toEqual(["A", "B"])
  })
})

describe("defaultBy", () => {
  it("is group when the devices fall into two groups or more", () => {
    expect(defaultBy([device(1, { group: group(1, "A"), groupId: 1 }), device(2)])).toBe("group")
  })
  it("is kind when nothing is grouped, one big cluster says nothing", () => {
    expect(defaultBy([device(1), device(2)])).toBe("kind")
    expect(defaultBy([])).toBe("kind")
  })
})

describe("layoutClusters", () => {
  const many = Array.from({ length: 30 }, (_, i) =>
    device(i + 1, { group: group(i + 1, `G${i + 1}`), groupId: i + 1 }),
  )

  it("puts no two clusters on one another, collapsed or open", () => {
    const clusters = buildClusters(many, "group")
    const open = new Set([clusters[0]!.key, clusters[5]!.key])
    const places = [...layoutClusters(clusters, open).values()]
    for (let i = 0; i < places.length; i++) {
      for (let j = i + 1; j < places.length; j++) {
        const a = places[i]!
        const b = places[j]!
        const apart = a.x + a.w <= b.x || b.x + b.w <= a.x || a.y + a.h <= b.y || b.y + b.h <= a.y
        expect(apart, `${i} and ${j} overlap`).toBe(true)
      }
    }
  })

  it("starts a new row before a cluster would pass the row width", () => {
    const clusters = buildClusters(many, "group")
    for (const p of layoutClusters(clusters, new Set()).values()) {
      expect(p.x + p.w).toBeLessThanOrEqual(ROW_WIDTH)
    }
  })

  it("is the same for the same input", () => {
    const clusters = buildClusters(many, "group")
    expect([...layoutClusters(clusters, new Set()).entries()]).toEqual([
      ...layoutClusters(clusters, new Set()).entries(),
    ])
  })
})

describe("buildClusterGraph", () => {
  const g1 = group(1, "Office")
  const g2 = group(2, "Cameras")
  const devices = [
    device(1, { group: g1, groupId: 1 }),
    device(2, { group: g1, groupId: 1 }),
    device(3, { group: g2, groupId: 2 }),
    device(4, { group: g2, groupId: 2 }),
  ]
  const links = [link(1, 1, 2), link(2, 1, 3), link(3, 2, 4), link(4, 3, 4)]

  it("makes a card for every closed cluster and no device nodes", () => {
    const { nodes } = buildClusterGraph(
      { devices, links, containers: [] },
      "group",
      new Set(),
      noop,
    )
    expect(nodes.map((n) => [n.id, n.type])).toEqual([
      ["cluster-g2", "cluster"], // equal sizes: Cameras before Office
      ["cluster-g1", "cluster"],
    ])
  })

  it("counts the links between two closed clusters as one edge and drops the inner ones", () => {
    const { edges } = buildClusterGraph(
      { devices, links, containers: [] },
      "group",
      new Set(),
      noop,
    )
    expect(edges).toHaveLength(1)
    expect(edges[0]).toMatchObject({
      source: "cluster-g1",
      target: "cluster-g2",
      label: "2",
      data: { count: 2 },
    })
  })

  it("opens a cluster into a frame with its devices inside it", () => {
    const { nodes } = buildClusterGraph(
      { devices, links, containers: [] },
      "group",
      new Set(["g1"]),
      noop,
    )
    const box = nodes.find((n) => n.id === "cluster-g1")!
    expect(box.type).toBe("clusterBox")
    const inside = nodes.filter((n) => n.parentId === "cluster-g1")
    expect(inside.map((n) => n.id)).toEqual(["1", "2"])
    for (const n of inside) {
      expect(n.extent).toBe("parent")
      expect(n.position.x + 192).toBeLessThanOrEqual(box.width!)
      expect(n.position.y + 92).toBeLessThanOrEqual(box.height!)
    }
  })

  it("draws a real link between two devices that are both on the screen", () => {
    const { edges } = buildClusterGraph(
      { devices, links, containers: [] },
      "group",
      new Set(["g1"]),
      noop,
    )
    expect(edges.find((e) => e.id === "link-1")).toMatchObject({
      source: "1",
      target: "2",
      label: "p1 - p2",
    })
  })

  it("joins an open device to a closed cluster, one edge per pair", () => {
    const { edges } = buildClusterGraph(
      { devices, links, containers: [] },
      "group",
      new Set(["g1"]),
      noop,
    )
    const toCluster = edges.filter((e) => e.target === "cluster-g2" || e.source === "cluster-g2")
    expect(toCluster.map((e) => [e.source, e.target, e.label].join(" "))).toEqual([
      "1 cluster-g2 1",
      "2 cluster-g2 1",
    ])
  })

  it("leaves out a link whose device is not in the list", () => {
    const { edges } = buildClusterGraph(
      { devices, links: [link(9, 1, 99)], containers: [] },
      "group",
      new Set(),
      noop,
    )
    expect(edges).toEqual([])
  })

  it("gives the toggle to every node", () => {
    const calls: string[] = []
    const { nodes } = buildClusterGraph(
      { devices, links, containers: [] },
      "group",
      new Set(),
      (k) => calls.push(k),
    )
    ;(nodes[0]!.data as { onToggle: (k: string) => void }).onToggle("g1")
    expect(calls).toEqual(["g1"])
  })
})

describe("sizes and keys", () => {
  it("uses about as many columns as rows", () => {
    expect(boxColumns(1)).toBe(1)
    expect(boxColumns(9)).toBe(3)
    expect(boxColumns(10)).toBe(4)
    const { w, h } = boxSize(100)
    expect(w).toBeGreaterThan(0)
    expect(h).toBeGreaterThan(0)
  })
  it("names a cluster the same way everywhere", () => {
    expect(clusterNodeId("g3")).toBe("cluster-g3")
    expect(clusterKeyOf(device(1, { group: group(3, "X"), groupId: 3 }), "group")).toBe("g3")
    expect(clusterKeyOf(device(1), "group")).toBe("none")
    expect(clusterKeyOf(device(1, { kind: "router" }), "kind")).toBe("krouter")
  })
})

describe("containers in the cluster view", () => {
  const g1 = group(1, "Office")
  const devices = [
    device(1, { group: g1, groupId: 1 }),
    device(2, { group: g1, groupId: 1 }),
    device(3),
  ]
  const containers = [container("c1", 1, "web"), container("c2", 1, "db"), container("c3", 3)]

  it("counts the containers of each cluster, on the card of a closed one", () => {
    const clusters = buildClusters(devices, "group", containers)
    expect(clusters.map((c) => [c.key, c.devices.length, c.containers])).toEqual([
      ["g1", 2, 2],
      ["none", 1, 1],
    ])
  })

  it("leaves a container out whose device is not in the list", () => {
    const clusters = buildClusters(devices, "group", [container("x", 99)])
    expect(clusters.every((c) => c.containers === 0)).toBe(true)
  })

  it("draws no container while a cluster is closed", () => {
    const { nodes, edges } = buildClusterGraph(
      { devices, links: [], containers },
      "group",
      new Set(),
      noop,
    )
    expect(nodes.every((n) => n.type === "cluster")).toBe(true)
    expect(edges).toEqual([])
  })

  it("puts the containers of a device in the cells after it, with an edge to the host", () => {
    const { nodes, edges } = buildClusterGraph(
      { devices, links: [], containers },
      "group",
      new Set(["g1"]),
      noop,
    )
    const inside = nodes.filter((n) => n.parentId === "cluster-g1")
    // device 1, its two containers (by name: db, web), then device 2
    expect(inside.map((n) => n.id)).toEqual(["1", "container-c2", "container-c1", "2"])
    expect(edges.map((e) => [e.source, e.target])).toEqual([
      ["1", "container-c2"],
      ["1", "container-c1"],
    ])
    expect(inside[0]!.data).toMatchObject({ containerCount: 2 })
    expect(inside[1]!.type).toBe("container")
  })

  it("makes room for the containers in the frame", () => {
    const withBoxes = buildClusterGraph(
      { devices, links: [], containers },
      "group",
      new Set(["g1"]),
      noop,
    )
    const without = buildClusterGraph(
      { devices, links: [], containers: [] },
      "group",
      new Set(["g1"]),
      noop,
    )
    const area = (n: { width?: number; height?: number }) => n.width! * n.height!
    expect(area(withBoxes.nodes[0]!)).toBeGreaterThan(area(without.nodes[0]!))
    const box = withBoxes.nodes[0]!
    for (const n of withBoxes.nodes.filter((x) => x.parentId)) {
      expect(n.position.x + 192).toBeLessThanOrEqual(box.width!)
      expect(n.position.y + 92).toBeLessThanOrEqual(box.height!)
    }
  })

  it("does not show the containers of a closed cluster that is linked to an open one", () => {
    const { nodes } = buildClusterGraph(
      { devices, links: [link(1, 1, 3)], containers },
      "group",
      new Set(["g1"]),
      noop,
    )
    expect(nodes.some((n) => n.id === "container-c3")).toBe(false)
  })
})

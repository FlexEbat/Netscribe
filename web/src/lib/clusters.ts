import type { Device, DeviceKind, Link, Topology } from "@/api/client"
import { kindLabels } from "@/lib/topology"
import { t } from "@/i18n"

/** What the devices are gathered by. */
export type ClusterBy = "group" | "kind"

/** Above this many devices the topology opens as clusters until the visitor chooses. */
export const CLUSTER_AUTO_ABOVE = 60

export interface Cluster {
  /** Stable across loads: `g<id>` for a group, `none` for no group, `k<kind>` for a kind. */
  key: string
  label: string
  vlan: number | null
  devices: Device[]
  online: number
  /** The kinds in the cluster with their counts, the most frequent first. */
  kinds: [DeviceKind, number][]
}

/** The key of the cluster a device belongs to. */
export function clusterKeyOf(d: Device, by: ClusterBy): string {
  return keyOf(d, by).key
}

function keyOf(d: Device, by: ClusterBy): { key: string; label: string; vlan: number | null } {
  if (by === "kind") return { key: `k${d.kind}`, label: kindLabels[d.kind], vlan: null }
  if (d.group) return { key: `g${d.group.id}`, label: d.group.name, vlan: d.group.vlan }
  return { key: "none", label: t("cluster.noGroup"), vlan: null }
}

/** Gathers the devices, the largest cluster first, equal sizes by name. */
export function buildClusters(devices: readonly Device[], by: ClusterBy): Cluster[] {
  const byKey = new Map<string, Cluster>()
  for (const d of devices) {
    const k = keyOf(d, by)
    let c = byKey.get(k.key)
    if (!c) {
      c = { key: k.key, label: k.label, vlan: k.vlan, devices: [], online: 0, kinds: [] }
      byKey.set(k.key, c)
    }
    c.devices.push(d)
    if (d.online) c.online++
  }
  for (const c of byKey.values()) {
    const counts = new Map<DeviceKind, number>()
    for (const d of c.devices) counts.set(d.kind, (counts.get(d.kind) ?? 0) + 1)
    c.kinds = [...counts.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
  }
  return [...byKey.values()].sort(
    (a, b) => b.devices.length - a.devices.length || a.label.localeCompare(b.label),
  )
}

/** Groups when the devices fall into at least two of them, else kinds: one big cluster says nothing. */
export function defaultBy(devices: readonly Device[]): ClusterBy {
  return new Set(devices.map((d) => keyOf(d, "group").key)).size >= 2 ? "group" : "kind"
}

export const clusterNodeId = (key: string) => `cluster-${key}`

// The size of what is drawn. A device card is 192 px wide (w-48) and about 92 px high.
export const COLLAPSED_W = 232
export const COLLAPSED_H = 136
const CELL_W = 224
const CELL_H = 120
const BOX_PAD = 24
const BOX_HEADER = 68
const GAP = 40
/** The width a row of clusters may fill before the next one starts a new row. */
export const ROW_WIDTH = 2400

export interface ClusterNodeData extends Record<string, unknown> {
  cluster: Cluster
  expanded: boolean
  onToggle: (key: string) => void
}

export interface ClusterFlowNode {
  id: string
  type: "cluster" | "clusterBox" | "device"
  position: { x: number; y: number }
  data: Record<string, unknown>
  parentId?: string
  extent?: "parent"
  draggable: false
  connectable?: boolean
  selectable?: boolean
  width?: number
  height?: number
  style?: Record<string, string | number>
}

export interface ClusterFlowEdge {
  id: string
  source: string
  target: string
  label: string
  data: { linkId?: number; count?: number }
}

/** How many columns an expanded cluster of n devices uses: about square. */
export function boxColumns(n: number): number {
  return Math.max(1, Math.ceil(Math.sqrt(n)))
}

export function boxSize(n: number): { w: number; h: number } {
  const cols = boxColumns(n)
  const rows = Math.ceil(n / cols)
  return { w: cols * CELL_W + BOX_PAD * 2, h: rows * CELL_H + BOX_PAD * 2 + BOX_HEADER }
}

/** Where each cluster stands: row after row, left to right, no two touching. */
export function layoutClusters(
  clusters: readonly Cluster[],
  expanded: ReadonlySet<string>,
): Map<string, { x: number; y: number; w: number; h: number }> {
  const out = new Map<string, { x: number; y: number; w: number; h: number }>()
  let x = 0
  let y = 0
  let rowH = 0
  for (const c of clusters) {
    const size = expanded.has(c.key)
      ? boxSize(c.devices.length)
      : { w: COLLAPSED_W, h: COLLAPSED_H }
    if (x > 0 && x + size.w > ROW_WIDTH) {
      x = 0
      y += rowH + GAP
      rowH = 0
    }
    out.set(c.key, { x, y, w: size.w, h: size.h })
    x += size.w + GAP
    rowH = Math.max(rowH, size.h)
  }
  return out
}

/**
 * The canvas of the cluster view: a card for every collapsed cluster, a box with its devices
 * in a grid for every expanded one, and the links between them. A link between two collapsed
 * clusters becomes one edge with a count; a link inside a collapsed cluster is not drawn.
 */
export function buildClusterGraph(
  topology: Pick<Topology, "devices" | "links">,
  by: ClusterBy,
  expanded: ReadonlySet<string>,
  onToggle: (key: string) => void,
): { nodes: ClusterFlowNode[]; edges: ClusterFlowEdge[]; clusters: Cluster[] } {
  const clusters = buildClusters(topology.devices, by)
  const places = layoutClusters(clusters, expanded)
  const clusterOfDevice = new Map<number, string>()
  const nodes: ClusterFlowNode[] = []

  for (const c of clusters) {
    const place = places.get(c.key)!
    const open = expanded.has(c.key)
    for (const d of c.devices) clusterOfDevice.set(d.id, c.key)
    const data: ClusterNodeData = { cluster: c, expanded: open, onToggle }
    nodes.push({
      id: clusterNodeId(c.key),
      type: open ? "clusterBox" : "cluster",
      position: { x: place.x, y: place.y },
      data,
      draggable: false,
      connectable: !open ? true : false,
      selectable: false,
      width: place.w,
      height: place.h,
      style: { width: place.w, height: place.h },
    })
    if (!open) continue
    const cols = boxColumns(c.devices.length)
    c.devices.forEach((d, i) => {
      nodes.push({
        id: String(d.id),
        type: "device",
        position: {
          x: BOX_PAD + (i % cols) * CELL_W,
          y: BOX_HEADER + BOX_PAD + Math.floor(i / cols) * CELL_H,
        },
        parentId: clusterNodeId(c.key),
        extent: "parent",
        draggable: false,
        connectable: false,
        data: { device: d, containerCount: 0 },
      })
    })
  }

  const endpoint = (deviceId: number): string | null => {
    const key = clusterOfDevice.get(deviceId)
    if (key === undefined) return null
    return expanded.has(key) ? String(deviceId) : clusterNodeId(key)
  }
  const edges: ClusterFlowEdge[] = []
  const counts = new Map<string, ClusterFlowEdge>()
  for (const l of topology.links as readonly Link[]) {
    const a = endpoint(l.aDeviceId)
    const b = endpoint(l.bDeviceId)
    if (a === null || b === null || a === b) continue
    if (!a.startsWith("cluster-") && !b.startsWith("cluster-")) {
      edges.push({
        id: `link-${l.id}`,
        source: a,
        target: b,
        label: [l.aPort, l.bPort].filter(Boolean).join(" - "),
        data: { linkId: l.id },
      })
      continue
    }
    const [s, tg] = a < b ? [a, b] : [b, a]
    const id = `agg-${s}-${tg}`
    const e = counts.get(id)
    if (e) {
      e.data.count = (e.data.count ?? 0) + 1
      e.label = String(e.data.count)
    } else {
      const edge: ClusterFlowEdge = { id, source: s, target: tg, label: "1", data: { count: 1 } }
      counts.set(id, edge)
      edges.push(edge)
    }
  }
  return { nodes, edges, clusters }
}

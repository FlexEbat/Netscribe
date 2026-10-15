import type { Container, Device, DeviceKind, Link, Topology } from "@/api/client"
import { stateStroke } from "@/lib/containers"

export interface DeviceNodeData extends Record<string, unknown> {
  device: Device
  containerCount: number
}

export interface CanvasNode {
  id: string
  type: "device"
  position: { x: number; y: number }
  data: DeviceNodeData
}

export interface CanvasEdge {
  id: string
  source: string
  target: string
  label: string
  data: { linkId: number; source: string }
}

/** The name a device is shown under: the label somebody set, else the hostname, else the IP. */
export function displayName(d: Pick<Device, "label" | "hostname" | "ip" | "mac">): string {
  return d.label || d.hostname || d.ip || d.mac || "device"
}

export function toNodes(topology: Topology): CanvasNode[] {
  const containers = new Map<number, number>()
  for (const c of topology.containers) {
    containers.set(c.deviceId, (containers.get(c.deviceId) ?? 0) + 1)
  }
  return topology.devices.map((device) => ({
    id: String(device.id),
    type: "device",
    position: { x: device.x ?? 0, y: device.y ?? 0 },
    data: { device, containerCount: containers.get(device.id) ?? 0 },
  }))
}

export function toEdges(links: readonly Link[]): CanvasEdge[] {
  return links.map((l) => ({
    id: `link-${l.id}`,
    source: String(l.aDeviceId),
    target: String(l.bDeviceId),
    label: [l.aPort, l.bPort].filter(Boolean).join(" - "),
    data: { linkId: l.id, source: l.source },
  }))
}

/**
 * Brings the nodes on screen up to date with a fresh topology without moving any node
 * that is already there: a node dragged but not yet saved stays where it is, and a
 * device that appeared is added without shifting the others.
 */
export function mergeNodes(
  current: readonly CanvasNode[],
  next: readonly CanvasNode[],
): CanvasNode[] {
  const onScreen = new Map(current.map((n) => [n.id, n]))
  return next.map((n) => {
    const existing = onScreen.get(n.id)
    return existing ? { ...n, position: existing.position } : n
  })
}

export const kindLabels: Record<DeviceKind, string> = {
  router: "Router",
  switch: "Switch",
  ap: "Access point",
  firewall: "Firewall",
  server: "Server",
  nas: "NAS",
  printer: "Printer",
  camera: "Camera",
  iot: "IoT",
  host: "Host",
  unknown: "Unknown",
}

/** Containers stand under their host, this many to a row. The numbers mirror render/layout.go. */
export const containersPerRow = 6
export const containerStepX = 150
export const containerStepY = 76
export const containerOffsetY = 110

export interface ContainerNodeData extends Record<string, unknown> {
  container: Container
  /** The id of the canvas node of the host device. */
  hostId: string
  /** The place of the container among the containers of its host. */
  index: number
}

export interface ContainerCanvasNode {
  id: string
  type: "container"
  position: { x: number; y: number }
  draggable: false
  connectable: false
  data: ContainerNodeData
}

export interface ContainerCanvasEdge {
  id: string
  source: string
  target: string
  style: { stroke: string }
  data: { containerId: string }
}

export type AnyCanvasNode = CanvasNode | ContainerCanvasNode

export function isContainerNode(n: { type?: string }): n is ContainerCanvasNode {
  return n.type === "container"
}

export function containerNodeId(containerId: string): string {
  return `container-${containerId}`
}

/** Where the index-th container of a host stands, from the position of the host. */
export function containerPosition(
  host: { x: number; y: number },
  index: number,
): { x: number; y: number } {
  return {
    x: host.x + (index % containersPerRow) * containerStepX,
    y: host.y + containerOffsetY + Math.floor(index / containersPerRow) * containerStepY,
  }
}

/** The containers of one host in the order they stand in: by name, then by id. */
export function sortContainers(list: readonly Container[]): Container[] {
  return [...list].sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id))
}

/** One node per container, under the node of its host. A container whose host is not on the canvas is left out. */
export function toContainerNodes(
  topology: Topology,
  hosts: ReadonlyMap<string, { x: number; y: number }>,
): ContainerCanvasNode[] {
  const byHost = new Map<string, Container[]>()
  for (const c of topology.containers) {
    const key = String(c.deviceId)
    byHost.set(key, [...(byHost.get(key) ?? []), c])
  }
  const out: ContainerCanvasNode[] = []
  for (const [hostId, list] of byHost) {
    const host = hosts.get(hostId)
    if (!host) continue
    sortContainers(list).forEach((container, index) => {
      out.push({
        id: containerNodeId(container.id),
        type: "container",
        position: containerPosition(host, index),
        draggable: false,
        connectable: false,
        data: { container, hostId, index },
      })
    })
  }
  return out
}

/** An edge from every host to each of its containers, coloured by the state of the container. */
export function toContainerEdges(containers: readonly Container[]): ContainerCanvasEdge[] {
  return containers.map((c) => ({
    id: `container-edge-${c.id}`,
    source: String(c.deviceId),
    target: containerNodeId(c.id),
    style: { stroke: stateStroke(c.state) },
    data: { containerId: c.id },
  }))
}

/**
 * Every node of a topology: the devices, kept where they are on screen, and the
 * containers under their hosts. Containers follow the host as it stands now.
 */
export function buildNodes(
  topology: Topology,
  current: readonly AnyCanvasNode[] = [],
): AnyCanvasNode[] {
  const devices = mergeNodes(
    current.filter((n): n is CanvasNode => !isContainerNode(n)),
    toNodes(topology),
  )
  const hosts = new Map(devices.map((n) => [n.id, n.position]))
  return [...devices, ...toContainerNodes(topology, hosts)]
}

/** Moves the containers of a host along with it while it is dragged. */
export function followHost(
  nodes: readonly AnyCanvasNode[],
  hostId: string,
  position: { x: number; y: number },
): AnyCanvasNode[] {
  return nodes.map((n) =>
    isContainerNode(n) && n.data.hostId === hostId
      ? { ...n, position: containerPosition(position, n.data.index) }
      : n,
  )
}

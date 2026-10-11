import type { Device, DeviceKind, Link, Topology } from "@/api/client"

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

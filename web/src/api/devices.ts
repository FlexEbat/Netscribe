import { api } from "./client"

export type DeviceKind =
  | "router"
  | "switch"
  | "ap"
  | "firewall"
  | "server"
  | "nas"
  | "printer"
  | "camera"
  | "iot"
  | "host"
  | "unknown"

export interface Device {
  id: number
  mac: string
  ip: string
  hostname: string
  vendor: string
  kind: DeviceKind
  description: string
  source: string
  online: boolean
  firstSeenAt: string
  lastSeenAt: string
}

export async function listDevices(): Promise<Device[]> {
  const body = await api<{ devices: Device[] }>("GET", "/api/devices")
  return body.devices
}

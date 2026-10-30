import type { Role, User } from "@/lib/permissions"

export class ApiError extends Error {
  readonly status: number
  /** Seconds to wait, from a Retry-After header. */
  readonly retryAfter: number | null

  constructor(status: number, message: string, retryAfter: number | null = null) {
    super(message)
    this.name = "ApiError"
    this.status = status
    this.retryAfter = retryAfter
  }
}

let csrfToken = ""
let onUnauthorized: (() => void) | null = null

/** The session's CSRF token, sent with every request that can change state. */
export function setCsrfToken(token: string): void {
  csrfToken = token
}

/** Called when a request that needed a session gets 401, so the interface can sign out. */
export function setUnauthorizedHandler(handler: (() => void) | null): void {
  onUnauthorized = handler
}

const safeMethods = new Set(["GET", "HEAD", "OPTIONS"])

interface Options {
  body?: unknown
  /** Requests that are allowed to answer 401 without ending the session, such as the sign-in itself. */
  expectUnauthorized?: boolean
}

export async function api<T = void>(
  method: string,
  path: string,
  { body, expectUnauthorized = false }: Options = {},
): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" }
  if (body !== undefined) headers["Content-Type"] = "application/json"
  if (!safeMethods.has(method.toUpperCase()) && csrfToken) headers["X-CSRF-Token"] = csrfToken

  let response: Response
  try {
    response = await fetch(path, {
      method,
      headers,
      credentials: "same-origin",
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError(0, "Cannot reach the server")
  }

  if (response.status === 401 && !expectUnauthorized) onUnauthorized?.()
  if (!response.ok) {
    throw new ApiError(response.status, await errorMessage(response), retryAfter(response))
  }
  // A 204, or a 202 that carries no body, has nothing to parse.
  if (response.status === 204) return undefined as T
  const text = await response.text()
  if (text === "") return undefined as T
  return JSON.parse(text) as T
}

async function errorMessage(response: Response): Promise<string> {
  try {
    const data: unknown = await response.json()
    if (
      typeof data === "object" &&
      data !== null &&
      "error" in data &&
      typeof data.error === "string"
    ) {
      return data.error
    }
  } catch {
    // The body is not JSON: fall through to the generic text.
  }
  return `Request failed (${response.status})`
}

function retryAfter(response: Response): number | null {
  const value = Number(response.headers.get("Retry-After"))
  return Number.isFinite(value) && value > 0 ? value : null
}

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
  /** The system nmap guessed, such as "Linux 5.3 - 5.4". Empty when nmap never ran. */
  osGuess: string
  source: string
  online: boolean
  manual: boolean
  label: string
  notes: string
  kindLocked: boolean
  /** A person marked the device as known. A scan creates devices untrusted. */
  trusted: boolean
  version: number
  firstSeenAt: string
  lastSeenAt: string
  x: number | null
  y: number | null
}

export interface DeviceQuery {
  q?: string
  online?: boolean
  trusted?: boolean
}

export function listDevices(query: DeviceQuery = {}): Promise<Device[]> {
  const params = new URLSearchParams()
  if (query.q) params.set("q", query.q)
  if (query.online !== undefined) params.set("online", String(query.online))
  if (query.trusted !== undefined) params.set("trusted", String(query.trusted))
  const suffix = params.size > 0 ? `?${params.toString()}` : ""
  return api<{ devices: Device[] }>("GET", `/api/devices${suffix}`).then((body) => body.devices)
}

export type ScanStatus = "running" | "done" | "failed"

export interface Scan {
  id: number
  target: string
  status: ScanStatus
  startedAt: string
  finishedAt: string | null
  error: string
  deviceCount: number
  linkCount: number
  containerCount: number
}

export async function listScans(): Promise<Scan[]> {
  const body = await api<{ scans: Scan[] }>("GET", "/api/scans")
  return body.scans
}

/** Starts a scan. With nmap true the scan runs nmap for devices nothing else could type. */
export function startScan(nmap = false): Promise<Scan> {
  return api<Scan>("POST", "/api/scans", nmap ? { body: { nmap: true } } : {})
}

export interface NmapState {
  /** The program was found. */
  available: boolean
  /** discovery.nmap is on, so every scan runs nmap. */
  enabled: boolean
  reason: string
}

/** Asks nmap about one device in the background. The new data shows when the topology changes. */
export function identifyDevice(id: number): Promise<void> {
  return api<void>("POST", `/api/devices/${id}/identify`)
}

export function getNmap(): Promise<NmapState> {
  return api<NmapState>("GET", "/api/nmap")
}

export interface Link {
  id: number
  aDeviceId: number
  aPort: string
  bDeviceId: number
  bPort: string
  source: string
  lastSeenAt: string
}

export interface PortMapping {
  hostIp: string
  hostPort: number
  containerPort: number
  proto: string
}

export interface ContainerNetwork {
  name: string
  ip: string
}

export interface Container {
  id: string
  deviceId: number
  name: string
  image: string
  state: string
  composeProject: string
  ports: PortMapping[]
  networks: ContainerNetwork[]
  firstSeenAt: string
  lastSeenAt: string
}

/** Whether the Docker daemon can be read right now, and if not, why. */
export interface DockerState {
  enabled: boolean
  available: boolean
  reason: string
}

export interface ContainerList {
  containers: Container[]
  docker: DockerState
}

/**
 * Fetches an export as a file. The server answers an error with a message, which a plain
 * download link would save as the file, so the request is made here and the file is saved
 * only when it is one.
 */
export async function downloadExport(format: string): Promise<{ blob: Blob; filename: string }> {
  let response: Response
  try {
    response = await fetch(`/api/export/${format}`, { credentials: "same-origin" })
  } catch {
    throw new ApiError(0, "Cannot reach the server")
  }
  if (response.status === 401) onUnauthorized?.()
  if (!response.ok) throw new ApiError(response.status, await errorMessage(response))
  const name = /filename="?([^";]+)"?/.exec(response.headers.get("Content-Disposition") ?? "")?.[1]
  return { blob: await response.blob(), filename: name ?? `netscribe.${format}` }
}

export type ChangeKind =
  | "device_added"
  | "device_online"
  | "device_offline"
  | "device_changed"
  | "link_added"
  | "link_removed"
  | "container_added"
  | "container_removed"

export interface Change {
  id: number
  scanId: number
  kind: ChangeKind
  entityId: string
  summary: string
  at: string
}

/** The newest changes first. */
export async function listChanges(limit = 200): Promise<Change[]> {
  const body = await api<{ changes: Change[] }>("GET", `/api/changes?limit=${limit}`)
  return body.changes
}

export function listContainers(): Promise<ContainerList> {
  return api<ContainerList>("GET", "/api/containers")
}

export interface Topology {
  devices: Device[]
  links: Link[]
  containers: Container[]
}

export function getTopology(): Promise<Topology> {
  return api<Topology>("GET", "/api/topology")
}

export function moveDevice(id: number, x: number, y: number): Promise<void> {
  return api("PUT", `/api/devices/${id}/position`, { body: { x, y } })
}

/** A network interface read from a device over SNMP. */
export interface DeviceInterface {
  id: number
  deviceId: number
  name: string
  mac: string
  ip: string
  up: boolean
}

/** An open port of a device. */
export interface Service {
  id: number
  deviceId: number
  port: number
  proto: string
  name: string
  /** What the service said about itself, such as the HTTP Server header. May be empty. */
  banner: string
  firstSeenAt: string
  lastSeenAt: string
  /** Why the open port deserves a look, or null when it is not on the list. */
  risk: Risk | null
}

export interface Risk {
  severity: "medium" | "high"
  message: string
}

export interface DeviceDetail {
  device: Device
  interfaces: DeviceInterface[]
  services: Service[]
}

export async function listServices(deviceId?: number): Promise<Service[]> {
  const suffix = deviceId === undefined ? "" : `?deviceId=${deviceId}`
  const body = await api<{ services: Service[] }>("GET", `/api/services${suffix}`)
  return body.services
}

export async function getDeviceDetail(id: number): Promise<DeviceDetail> {
  const body = await api<{
    device: Device
    interfaces?: DeviceInterface[]
    services?: Service[]
  }>("GET", `/api/devices/${id}`)
  return { device: body.device, interfaces: body.interfaces ?? [], services: body.services ?? [] }
}

export async function getDevice(id: number): Promise<Device> {
  return (await getDeviceDetail(id)).device
}

export interface DeviceInput {
  label: string
  hostname: string
  notes: string
  kind: DeviceKind
  ip: string
  mac: string
}

export async function createDevice(input: DeviceInput): Promise<Device> {
  const body = await api<{ device: Device }>("POST", "/api/devices", { body: input })
  return body.device
}

/** The fields to change; only the version is required. */
export interface DeviceChanges {
  label?: string
  notes?: string
  kind?: DeviceKind
  trusted?: boolean
  version: number
}

export async function updateDevice(id: number, changes: DeviceChanges): Promise<Device> {
  const body = await api<{ device: Device }>("PATCH", `/api/devices/${id}`, { body: changes })
  return body.device
}

export function deleteDevice(id: number): Promise<void> {
  return api("DELETE", `/api/devices/${id}`)
}

export interface LinkInput {
  aDeviceId: number
  aPort: string
  bDeviceId: number
  bPort: string
}

export async function createLink(input: LinkInput): Promise<Link> {
  const body = await api<{ link: Link }>("POST", "/api/links", { body: input })
  return body.link
}

export function deleteLink(id: number): Promise<void> {
  return api("DELETE", `/api/links/${id}`)
}

export async function listUsers(): Promise<User[]> {
  const body = await api<{ users: User[] }>("GET", "/api/users")
  return body.users
}

export async function createUser(input: {
  username: string
  role: Role
  password: string
}): Promise<User> {
  const body = await api<{ user: User }>("POST", "/api/users", { body: input })
  return body.user
}

export async function updateUser(
  id: number,
  changes: { role?: Role; disabled?: boolean },
): Promise<User> {
  const body = await api<{ user: User }>("PATCH", `/api/users/${id}`, { body: changes })
  return body.user
}

export function resetPassword(id: number, password: string): Promise<void> {
  return api("PUT", `/api/users/${id}/password`, { body: { password } })
}

export function deleteUser(id: number): Promise<void> {
  return api("DELETE", `/api/users/${id}`)
}

export interface AccessToken {
  id: number
  name: string
  prefix: string
  scope: "read" | "write"
  createdAt: string
  expiresAt: string
  lastUsedAt: string | null
  revoked: boolean
}

export async function listTokens(): Promise<AccessToken[]> {
  const body = await api<{ tokens: AccessToken[] }>("GET", "/api/tokens")
  return body.tokens
}

export function createToken(input: {
  name: string
  scope: "read" | "write"
  days: number
}): Promise<{ value: string; token: AccessToken }> {
  return api("POST", "/api/tokens", { body: input })
}

export function revokeToken(id: number): Promise<void> {
  return api("DELETE", `/api/tokens/${id}`)
}

export interface AuditEntry {
  id: number
  at: string
  userId: number | null
  username: string
  action: string
  entity: string
  entityId: string
  result: "ok" | "denied" | "error"
  ip: string
  detail: string
}

export interface AuditQuery {
  user?: string
  action?: string
  result?: string
  from?: string
  to?: string
  before?: number
  limit?: number
}

export async function listAudit(query: AuditQuery = {}): Promise<AuditEntry[]> {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== "") params.set(key, String(value))
  }
  const suffix = params.size > 0 ? `?${params.toString()}` : ""
  const body = await api<{ entries: AuditEntry[] }>("GET", `/api/audit${suffix}`)
  return body.entries
}

export interface UpdateRelease {
  tag: string
  version: string
  url: string
  notes: string
  publishedAt: string
}

export interface UpdateStatus {
  current: string
  latest: UpdateRelease | null
  available: boolean
  checking: boolean
  checkedAt: string
  error?: string
  releasesUrl: string
  changelogUrl: string
}

export interface HelperState {
  available: boolean
  /** idle, waiting, running, done or failed. */
  state: string
  action: string
  message: string
  at: string | null
}

export interface VersionInfo {
  version: string
  update: UpdateStatus
  helper: HelperState
}

export function getVersion(): Promise<VersionInfo> {
  return api<VersionInfo>("GET", "/api/version")
}

/** Asks GitHub now, which the server allows once in 30 seconds. */
export function checkForUpdate(): Promise<VersionInfo> {
  return api<VersionInfo>("POST", "/api/update/check")
}

/** Asks the helper to install the newest release. The service restarts when it is done. */
export function startUpdate(): Promise<VersionInfo> {
  return api<VersionInfo>("POST", "/api/update")
}

export function restartService(): Promise<VersionInfo> {
  return api<VersionInfo>("POST", "/api/system/restart")
}

export type SettingType =
  "bool" | "int" | "duration" | "text" | "list" | "ports" | "enum" | "secrets"

export interface Setting {
  key: string
  label: string
  help: string
  type: SettingType
  /** A boolean, a number, text or a list of text. Null for secrets, which are never sent. */
  value: boolean | number | string | string[] | null
  default: string
  choices?: string[]
  /** A running service applies a change by itself. */
  hot: boolean
  /** Secrets: how many are set. */
  count?: number
}

export interface SettingsGroup {
  name: string
  hint: string
  settings: Setting[]
}

export interface SettingsSaved {
  saved: string[]
  needRestart: string[]
}

export async function getSettings(): Promise<SettingsGroup[]> {
  const data = await api<{ groups: SettingsGroup[] }>("GET", "/api/settings")
  return data.groups
}

export function saveSettings(changes: Record<string, unknown>): Promise<SettingsSaved> {
  return api<SettingsSaved>("PUT", "/api/settings", { body: { changes } })
}

export interface NotificationChannel {
  name: string
  type: "webhook" | "telegram" | "email"
  /** The events the channel is sent. */
  events: string[]
}

/** The channels of the config. Addresses, tokens and passwords never come with them. */
export async function listNotifications(): Promise<NotificationChannel[]> {
  const body = await api<{ channels: NotificationChannel[] }>("GET", "/api/notifications")
  return body.channels
}

export function testNotification(channel: string): Promise<void> {
  return api("POST", "/api/notifications/test", { body: { channel } })
}

import type { Change, ChangeKind, Scan } from "@/api/client"

/** Plain words for the kinds of change. */
export const kindLabels: Record<ChangeKind, string> = {
  device_added: "Device added",
  device_online: "Device online",
  device_offline: "Device offline",
  device_changed: "Device changed",
  link_added: "Link added",
  link_removed: "Link removed",
  container_added: "Container added",
  container_removed: "Container removed",
}

/** Tailwind classes of a change: green for what appeared, red for what went away, amber for what changed. */
export function kindClasses(kind: string): string {
  switch (kind) {
    case "device_added":
    case "device_online":
    case "link_added":
    case "container_added":
      return "border-emerald-500/50 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400"
    case "device_offline":
    case "link_removed":
    case "container_removed":
      return "border-red-500/50 bg-red-500/10 text-red-700 dark:text-red-400"
    case "device_changed":
      return "border-amber-500/50 bg-amber-500/10 text-amber-700 dark:text-amber-400"
    default:
      return "border-border bg-muted text-muted-foreground"
  }
}

export interface ScanPoint {
  scanId: number
  /** When the scan finished, as the label of the axis. */
  label: string
  devices: number
}

/**
 * One point per completed scan, oldest first. A failed or running scan is not a point:
 * its numbers say nothing about the network.
 */
export function scanPoints(scans: readonly Scan[]): ScanPoint[] {
  return scans
    .filter((s) => s.status === "done")
    .sort((a, b) => a.id - b.id)
    .map((s) => ({
      scanId: s.id,
      label: new Date(s.finishedAt ?? s.startedAt).toLocaleString(),
      devices: s.deviceCount,
    }))
}

/** Counts the changes by kind, for the line above the feed. */
export function countByKind(changes: readonly Change[]): Map<ChangeKind, number> {
  const out = new Map<ChangeKind, number>()
  for (const c of changes) out.set(c.kind, (out.get(c.kind) ?? 0) + 1)
  return out
}

import type { Container, PortMapping } from "@/api/client"

/** Tailwind classes for the state of a container: green runs, amber is in between, red is down. */
export function stateClasses(state: string): string {
  switch (state) {
    case "running":
      return "border-emerald-500/50 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400"
    case "restarting":
    case "paused":
    case "removing":
      return "border-amber-500/50 bg-amber-500/10 text-amber-700 dark:text-amber-400"
    case "exited":
    case "dead":
      return "border-red-500/50 bg-red-500/10 text-red-700 dark:text-red-400"
    default:
      return "border-border bg-muted text-muted-foreground"
  }
}

/** The stroke colour of the canvas edge that leads to a container in the given state. */
export function stateStroke(state: string): string {
  switch (state) {
    case "running":
      return "#10b981"
    case "restarting":
    case "paused":
    case "removing":
      return "#f59e0b"
    case "exited":
    case "dead":
      return "#ef4444"
    default:
      return "#94a3b8"
  }
}

/** "8080->80/tcp" for a published port, "443/tcp" for one that is only exposed. */
export function formatPort(p: PortMapping): string {
  if (p.hostPort > 0) {
    const host = p.hostIp && p.hostIp !== "0.0.0.0" && p.hostIp !== "::" ? `${p.hostIp}:` : ""
    return `${host}${p.hostPort}->${p.containerPort}/${p.proto}`
  }
  return `${p.containerPort}/${p.proto}`
}

/**
 * The ports a container shows, without the IPv6 twin of a port that is already published
 * on IPv4 under the same numbers.
 */
export function uniquePorts(ports: readonly PortMapping[]): PortMapping[] {
  const seen = new Set<string>()
  const out: PortMapping[] = []
  for (const p of ports) {
    const key = `${p.hostPort}|${p.containerPort}|${p.proto}|${p.hostIp === "::" ? "0.0.0.0" : p.hostIp}`
    if (seen.has(key)) continue
    seen.add(key)
    out.push(p)
  }
  return out
}

export function filterContainers(list: readonly Container[], query: string): Container[] {
  const q = query.trim().toLowerCase()
  if (q === "") return [...list]
  return list.filter((c) =>
    [c.name, c.image, c.state, c.composeProject].some((v) => v.toLowerCase().includes(q)),
  )
}

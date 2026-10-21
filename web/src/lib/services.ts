import type { Service } from "@/api/client"

/**
 * Keeps the services whose name or banner contains the text (any case). A service is
 * also found by its port number, because a service without a name has nothing else.
 */
export function filterServices<T extends Pick<Service, "name" | "port" | "banner">>(
  services: readonly T[],
  query: string,
): T[] {
  const q = query.trim().toLowerCase()
  if (q === "") return [...services]
  return services.filter(
    (s) =>
      s.name.toLowerCase().includes(q) ||
      s.banner.toLowerCase().includes(q) ||
      String(s.port) === q,
  )
}

/** The label of a service: its name, or the port when it has none. */
export function serviceLabel(s: Pick<Service, "name" | "port">): string {
  return s.name || String(s.port)
}

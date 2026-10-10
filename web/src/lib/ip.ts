/** Compares dotted IPv4 addresses by value. Anything else sorts after valid addresses. */
export function compareIp(a: string, b: string): number {
  const pa = parseIpv4(a)
  const pb = parseIpv4(b)
  if (pa === null && pb === null) return 0
  if (pa === null) return 1
  if (pb === null) return -1
  return pa - pb
}

export function sortByIp<T extends { ip: string }>(items: readonly T[]): T[] {
  return [...items].sort((x, y) => compareIp(x.ip, y.ip))
}

function parseIpv4(ip: string): number | null {
  const parts = ip.split(".")
  if (parts.length !== 4) return null
  let value = 0
  for (const part of parts) {
    if (!/^\d{1,3}$/.test(part)) return null
    const n = Number(part)
    if (n > 255) return null
    value = value * 256 + n
  }
  return value
}

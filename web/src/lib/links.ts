/*
 * Links to a device's services. They are built from the stored address and port only,
 * never from a host name or a banner, which come from the network and may hold anything.
 * Only the schemes http and https are produced, and the one SSH helper makes a command to
 * copy, because a browser cannot open ssh:// safely everywhere.
 */

const webNames = new Set(["http", "https"])
const secureOnly = new Set([443, 8443])

/** True for a dotted IPv4 address or an IPv6 address, and for nothing else. */
export function isIpAddress(address: string): boolean {
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(address)) {
    return address.split(".").every((part) => Number(part) <= 255)
  }
  if (!address.includes(":") || !/^[0-9a-fA-F:.]+$/.test(address)) return false
  try {
    new URL(`http://[${address}]/`)
    return true
  } catch {
    return false
  }
}

/** True for a whole number from 1 to 65535. */
export function isPort(port: number): boolean {
  return Number.isInteger(port) && port >= 1 && port <= 65535
}

/** True when the port or the name of a service says it is a web page. */
function isWeb(port: number, name: string): boolean {
  return (
    port === 80 ||
    port === 443 ||
    (port >= 8000 && port <= 8099) ||
    port === 8443 ||
    port === 9000 ||
    webNames.has(name.trim().toLowerCase())
  )
}

/**
 * The web address of a service, or null when the service is not a web page or the address
 * or the port is not valid. The name is read only to tell a web service from another one.
 */
export function webUrl(address: string, port: number, name = ""): string | null {
  if (!isIpAddress(address) || !isPort(port) || !isWeb(port, name)) return null
  const named = name.trim().toLowerCase()
  const https = named === "https" || (named !== "http" && secureOnly.has(port))
  const scheme = https ? "https" : "http"
  const host = address.includes(":") ? `[${address}]` : address
  const standard = (https && port === 443) || (!https && port === 80)
  return `${scheme}://${host}${standard ? "" : `:${port}`}`
}

/** True when the service is SSH, by its port 22 or by its name. */
function isSsh(port: number, name: string): boolean {
  return port === 22 || name.trim().toLowerCase() === "ssh"
}

/**
 * The command that opens an SSH session to the service, or null when the service is not SSH
 * or the address or the port is not valid. A port other than 22 adds `-p`.
 */
export function sshCommand(address: string, port: number, name = ""): string | null {
  if (!isIpAddress(address) || !isPort(port) || !isSsh(port, name)) return null
  return port === 22 ? `ssh ${address}` : `ssh ${address} -p ${port}`
}

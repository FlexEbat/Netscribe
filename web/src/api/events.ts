import { api, type Scan } from "./client"

export interface ScanProgress {
  scanId: number
  stage: string
  done: number
  total: number
}

/** The data of a config.error event: why the config file was refused. */
export interface ConfigError {
  error: string
}

export interface EventHandlers {
  "scan.started"?: (scan: Scan) => void
  "scan.progress"?: (progress: ScanProgress) => void
  "scan.finished"?: (scan: Scan) => void
  "topology.changed"?: () => void
  "config.reloaded"?: () => void
  "config.error"?: (error: ConfigError) => void
}

/**
 * Opens the server's event stream and returns a function that closes it. The browser
 * reconnects by itself when the connection drops. Without EventSource support nothing
 * is opened: the interface then simply updates on reload.
 */
export function openEvents(handlers: EventHandlers): () => void {
  if (typeof EventSource === "undefined") return () => {}
  const source = new EventSource("/api/events")
  for (const name of Object.keys(handlers) as (keyof EventHandlers)[]) {
    source.addEventListener(name, (event) => {
      const handler = handlers[name] as ((data: unknown) => void) | undefined
      try {
        handler?.(JSON.parse((event as MessageEvent<string>).data) as unknown)
      } catch {
        // A message that is not valid JSON is ignored; the next one is unaffected.
      }
    })
  }
  source.onerror = () => {
    // The browser retries a dropped connection by itself but gives up on a refused one,
    // which is what happens after the server ended the stream because the session was
    // revoked. Ask which it was: a 401 reaches the unauthorized handler and signs the
    // interface out, any other answer changes nothing.
    if (source.readyState === EventSource.CLOSED) {
      api("GET", "/api/auth/me").catch(() => {})
    }
  }
  return () => source.close()
}

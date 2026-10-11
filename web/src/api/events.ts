import type { Scan } from "./client"

export interface ScanProgress {
  scanId: number
  stage: string
  done: number
  total: number
}

export interface EventHandlers {
  "scan.started"?: (scan: Scan) => void
  "scan.progress"?: (progress: ScanProgress) => void
  "scan.finished"?: (scan: Scan) => void
  "topology.changed"?: () => void
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
  return () => source.close()
}

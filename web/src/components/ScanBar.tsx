import { Button } from "@/components/ui/button"
import type { NmapState, Scan } from "@/api/client"
import type { ScanProgress } from "@/api/events"

interface ScanBarProps {
  scan: Scan | null
  progress: ScanProgress | null
  canRun: boolean
  starting: boolean
  error: string
  onStart: () => void
  /** What the server said about nmap, or null while it is unknown. */
  nmap?: NmapState | null
  useNmap?: boolean
  onUseNmapChange?: (on: boolean) => void
}

export function ScanBar({
  scan,
  progress,
  canRun,
  starting,
  error,
  onStart,
  nmap = null,
  useNmap = false,
  onUseNmapChange,
}: ScanBarProps) {
  const running = starting || scan?.status === "running"
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-lg border px-4 py-3 text-sm">
      <div className="flex min-w-0 flex-1 flex-col gap-1" aria-live="polite">
        <span>{summary(scan, running)}</span>
        {running && progress ? (
          <div
            role="progressbar"
            aria-label="Scan progress"
            aria-valuemin={0}
            aria-valuemax={progress.total}
            aria-valuenow={progress.done}
            className="h-1.5 w-full max-w-xs overflow-hidden rounded-full bg-muted"
          >
            <div
              className="h-full bg-primary transition-all"
              style={{ width: `${(progress.done / progress.total) * 100}%` }}
            />
          </div>
        ) : null}
        {error ? (
          <span role="alert" className="text-destructive">
            {error}
          </span>
        ) : null}
      </div>
      {canRun && nmap?.available ? (
        <label className="flex items-center gap-2 text-muted-foreground">
          <input
            type="checkbox"
            checked={nmap.enabled || useNmap}
            disabled={nmap.enabled || running}
            onChange={(e) => onUseNmapChange?.(e.target.checked)}
          />
          {nmap.enabled ? "Nmap (on in the config)" : "Identify unknown devices with Nmap"}
        </label>
      ) : null}
      {canRun ? (
        <Button size="sm" onClick={onStart} disabled={running}>
          {running ? "Scanning…" : "Run scan"}
        </Button>
      ) : null}
    </div>
  )
}

function summary(scan: Scan | null, running: boolean): string {
  if (running) return "Scan in progress"
  if (!scan) return "No scan has run yet"
  if (scan.status === "failed") {
    return `Last scan failed: ${scan.error || "unknown error"}`
  }
  const when = scan.finishedAt ? new Date(scan.finishedAt).toLocaleString() : ""
  return `Last scan: ${scan.deviceCount} devices${when ? `, ${when}` : ""}`
}

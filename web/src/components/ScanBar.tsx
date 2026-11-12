import { Button } from "@/components/ui/button"
import type { NmapState, Scan } from "@/api/client"
import type { ScanProgress } from "@/api/events"
import { formatDateTime, t } from "@/i18n"

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
    <div className="surface flex flex-wrap items-center gap-3 px-4 py-3 text-sm">
      <div className="flex min-w-[min(100%,16rem)] flex-1 flex-col gap-1" aria-live="polite">
        <span>{summary(scan, running)}</span>
        {running && progress ? (
          <div
            role="progressbar"
            aria-label={t("scanBar.scanProgress")}
            aria-valuemin={0}
            aria-valuemax={progress.total}
            aria-valuenow={progress.done}
            className="h-1.5 w-full max-w-xs overflow-hidden rounded-full bg-muted"
          >
            <div
              className="h-full w-full origin-left bg-primary transition-transform"
              style={{ transform: `scaleX(${progress.done / progress.total})` }}
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
          {t(nmap.enabled ? "scanBar.nmapOn" : "scanBar.nmapIdentify")}
        </label>
      ) : null}
      {canRun ? (
        <Button size="sm" onClick={onStart} disabled={running}>
          {t(running ? "scanBar.scanning" : "scanBar.runScan")}
        </Button>
      ) : null}
    </div>
  )
}

function summary(scan: Scan | null, running: boolean): string {
  if (running) return t("scanBar.inProgress")
  if (!scan) return t("scanBar.none")
  if (scan.status === "failed") {
    return t("scanBar.failed", {
      error: scan.error || t("scanBar.unknownError"),
    })
  }
  const n = scan.deviceCount
  return scan.finishedAt
    ? t("scanBar.lastWhen", { n, when: formatDateTime(scan.finishedAt) })
    : t("scanBar.last", { n })
}

import { useCallback, useEffect, useState } from "react"
import { ArrowUpCircle } from "lucide-react"
import { getVersion, type VersionInfo } from "@/api/client"
import { UpdateDialog } from "@/components/UpdateDialog"
import { can, type Permission } from "@/lib/permissions"
import { cn } from "@/lib/utils"

const refreshEvery = 10 * 60 * 1000

interface VersionBadgeProps {
  permissions: Permission[]
}

/**
 * The running version in the corner. When a newer release exists it glows, and a click asks
 * whether to update. A failed request shows nothing: the badge is a convenience.
 */
export function VersionBadge({ permissions }: VersionBadgeProps) {
  const [info, setInfo] = useState<VersionInfo | null>(null)
  const [open, setOpen] = useState(false)
  const onInfo = useCallback((next: VersionInfo) => setInfo(next), [])

  useEffect(() => {
    let alive = true
    const load = () => {
      getVersion()
        .then((next) => alive && setInfo(next))
        .catch(() => undefined)
    }
    load()
    const timer = window.setInterval(load, refreshEvery)
    return () => {
      alive = false
      window.clearInterval(timer)
    }
  }, [])

  if (!info) return null
  const newer = info.update.available && info.update.latest !== null
  const label = info.version === "dev" ? "dev build" : info.version

  return (
    <>
      <div className="pointer-events-none fixed right-4 bottom-4 z-30 pb-[env(safe-area-inset-bottom)]">
        {newer ? (
          <button
            type="button"
            onClick={() => setOpen(true)}
            aria-label={`Version ${label}. ${info.update.latest?.tag} is available. Open the update notice.`}
            className={cn(
              "update-glow pointer-events-auto inline-flex items-center gap-1.5 rounded-full border bg-popover px-3 py-1.5 font-mono text-xs text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
            )}
          >
            <ArrowUpCircle aria-hidden="true" strokeWidth={1.8} className="size-3.5 text-brand" />
            {label}
            <span className="text-brand">→ {info.update.latest?.tag}</span>
          </button>
        ) : (
          <span className="pointer-events-auto rounded-full border bg-popover/80 px-3 py-1.5 font-mono text-xs text-muted-foreground backdrop-blur">
            {label}
          </span>
        )}
      </div>
      {open && info.update.latest ? (
        <UpdateDialog
          info={info}
          canUpdate={can(permissions, "system:manage")}
          onClose={() => setOpen(false)}
          onInfo={onInfo}
        />
      ) : null}
    </>
  )
}

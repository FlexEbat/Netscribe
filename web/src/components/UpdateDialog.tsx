import { ArrowSquareOut } from "@phosphor-icons/react"
import { useEffect, useRef, useState } from "react"
import { ApiError, getVersion, startUpdate, type VersionInfo } from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"

interface UpdateDialogProps {
  info: VersionInfo
  /** Whether this person may start the update. */
  canUpdate: boolean
  onClose: () => void
  /** Called with fresh information while the update runs. */
  onInfo: (info: VersionInfo) => void
}

type Phase = "ask" | "working" | "done" | "failed"

const pollEvery = 2500
const giveUpAfter = 5 * 60 * 1000

/** Only a link to this project's releases is shown as a link. */
function safeLink(url: string): string | null {
  return url.startsWith("https://github.com/FlexEbat/Netscribe/") ? url : null
}

/** Shows what is new and asks whether to update. The answer is the person's, not the panel's. */
export function UpdateDialog({ info, canUpdate, onClose, onInfo }: UpdateDialogProps) {
  const [phase, setPhase] = useState<Phase>("ask")
  const [error, setError] = useState("")
  const started = useRef(0)
  const from = useRef(info.version)
  const latest = info.update.latest
  const release = latest ? safeLink(latest.url) : null
  const changelog = safeLink(info.update.releasesUrl)

  useEffect(() => {
    if (phase !== "working") return
    const timer = window.setInterval(() => {
      void (async () => {
        try {
          const next = await getVersion()
          onInfo(next)
          if (next.version !== from.current) {
            setPhase("done")
          } else if (next.helper.state === "failed") {
            setError(next.helper.message || "The update failed.")
            setPhase("failed")
          }
        } catch {
          // The service restarts as the last step of an update: no answer for a moment is normal.
        }
        if (Date.now() - started.current > giveUpAfter) {
          setError("The panel did not come back in time. Check the service with: sudo netscribe")
          setPhase("failed")
        }
      })()
    }, pollEvery)
    return () => window.clearInterval(timer)
  }, [phase, onInfo])

  async function update() {
    setError("")
    try {
      onInfo(await startUpdate())
      started.current = Date.now()
      from.current = info.version
      setPhase("working")
    } catch (e) {
      setError(
        e instanceof ApiError && e.status !== 0 ? e.message : "The update could not be started.",
      )
      setPhase("failed")
    }
  }

  const busy = phase === "working"
  return (
    <Dialog open onOpenChange={(open) => !open && !busy && onClose()}>
      <DialogContent aria-describedby={undefined} className="max-w-lg">
        <DialogTitle className="text-lg font-semibold">
          {phase === "done" ? "Netscribe is updated" : "A new version is available"}
        </DialogTitle>
        <DialogDescription className="text-sm text-muted-foreground">
          {info.version} → {latest?.tag ?? "newer"}
        </DialogDescription>

        {phase === "ask" || phase === "failed" ? (
          <>
            {latest?.notes ? (
              <Textarea
                readOnly
                aria-label="What is new"
                value={latest.notes}
                className="max-h-56 min-h-32 font-mono text-xs"
              />
            ) : null}
            <div className="flex flex-wrap gap-4 text-sm">
              {release ? (
                <a
                  href={release}
                  target="_blank"
                  rel="noreferrer noopener"
                  className="inline-flex items-center gap-1 text-brand underline-offset-4 hover:underline"
                >
                  This release <ArrowSquareOut aria-hidden="true" className="size-3.5" />
                </a>
              ) : null}
              {changelog ? (
                <a
                  href={changelog}
                  target="_blank"
                  rel="noreferrer noopener"
                  className="inline-flex items-center gap-1 text-brand underline-offset-4 hover:underline"
                >
                  All releases and changelog{" "}
                  <ArrowSquareOut aria-hidden="true" className="size-3.5" />
                </a>
              ) : null}
            </div>
            {error ? <Alert variant="destructive">{error}</Alert> : null}
            {!canUpdate ? (
              <Alert>Only an administrator can update. Ask one to open this notice.</Alert>
            ) : !info.helper.available ? (
              <Alert>
                This install cannot update itself. On the server, run{" "}
                <code className="font-mono">sudo netscribe</code> and choose Update, or{" "}
                <code className="font-mono">sudo netscribe update</code>. A Docker install updates
                by pulling the new image.
              </Alert>
            ) : (
              <p className="text-sm text-muted-foreground">
                Update now? The panel is unavailable for a moment while it restarts.
              </p>
            )}
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={onClose}>
                Not now
              </Button>
              {canUpdate && info.helper.available ? (
                <Button onClick={() => void update()}>Update now</Button>
              ) : null}
            </div>
          </>
        ) : null}

        {busy ? (
          <p role="status" className="text-sm">
            Updating to {latest?.tag}. The panel restarts when it is done; this page follows on its
            own.
          </p>
        ) : null}

        {phase === "done" ? (
          <>
            <p role="status" className="text-sm">
              The panel now runs {latest?.tag}. Reload the page to use it.
            </p>
            <div className="flex justify-end">
              <Button onClick={() => window.location.reload()}>Reload</Button>
            </div>
          </>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

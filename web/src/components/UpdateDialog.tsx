import { ArrowSquareOut } from "@phosphor-icons/react"
import { useEffect, useRef, useState } from "react"
import { ApiError, getVersion, startUpdate, type VersionInfo } from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { t } from "@/i18n"
import { tRich } from "@/i18n/rich"

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
          // The new version answering is not the end: the helper still checks it for a minute
          // and puts the old program back if it fails. Done means the helper has finished.
          const helperBusy = next.helper.state === "running" || next.helper.state === "waiting"
          if (next.version !== from.current && !helperBusy) {
            setPhase("done")
          } else if (next.helper.state === "failed" || next.helper.state === "rolled back") {
            setError(next.helper.message || t("updateDialog.failed"))
            setPhase("failed")
          }
        } catch {
          // The service restarts as the last step of an update: no answer for a moment is normal.
        }
        if (Date.now() - started.current > giveUpAfter) {
          setError(t("updateDialog.didNotComeBack"))
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
        e instanceof ApiError && e.status !== 0 ? e.message : t("updateDialog.couldNotStart"),
      )
      setPhase("failed")
    }
  }

  const busy = phase === "working"
  return (
    <Dialog open onOpenChange={(open) => !open && !busy && onClose()}>
      <DialogContent aria-describedby={undefined} className="max-w-lg">
        <DialogTitle className="text-lg font-semibold">
          {t(phase === "done" ? "updateDialog.updated" : "updateDialog.available")}
        </DialogTitle>
        <DialogDescription className="text-sm text-muted-foreground">
          {info.version} → {latest?.tag ?? t("updateDialog.newer")}
        </DialogDescription>

        {phase === "ask" || phase === "failed" ? (
          <>
            {latest?.notes ? (
              <Textarea
                readOnly
                aria-label={t("updateDialog.whatIsNew")}
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
                  {t("updateDialog.thisRelease")}{" "}
                  <ArrowSquareOut aria-hidden="true" className="size-3.5" />
                </a>
              ) : null}
              {changelog ? (
                <a
                  href={changelog}
                  target="_blank"
                  rel="noreferrer noopener"
                  className="inline-flex items-center gap-1 text-brand underline-offset-4 hover:underline"
                >
                  {t("updateDialog.changelog")}{" "}
                  <ArrowSquareOut aria-hidden="true" className="size-3.5" />
                </a>
              ) : null}
            </div>
            {error ? <Alert variant="destructive">{error}</Alert> : null}
            {!canUpdate ? (
              <Alert>{t("updateDialog.onlyAnAdministratorCanUpdate")}</Alert>
            ) : !info.helper.available ? (
              <Alert>
                {tRich("updateDialog.cannotSelfUpdate", {
                  run: <code className="font-mono">{t("updateDialog.sudoNetscribe")}</code>,
                  runUpdate: (
                    <code className="font-mono">{t("updateDialog.sudoNetscribeUpdate")}</code>
                  ),
                })}
              </Alert>
            ) : (
              <p className="text-sm text-muted-foreground">
                {t("updateDialog.updateNowThePanelIs")}
              </p>
            )}
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={onClose}>
                {t("updateDialog.notNow")}
              </Button>
              {canUpdate && info.helper.available ? (
                <Button onClick={() => void update()}>{t("updateDialog.updateNow")}</Button>
              ) : null}
            </div>
          </>
        ) : null}

        {busy ? (
          <p role="status" className="text-sm">
            {t("updateDialog.updating", { tag: latest?.tag ?? "" })}
          </p>
        ) : null}

        {phase === "done" ? (
          <>
            <p role="status" className="text-sm">
              {t("updateDialog.nowRuns", { tag: latest?.tag ?? "" })}
            </p>
            <div className="flex justify-end">
              <Button onClick={() => window.location.reload()}>{t("updateDialog.reload")}</Button>
            </div>
          </>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

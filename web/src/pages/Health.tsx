import { useCallback, useEffect, useState } from "react"
import {
  ApiError,
  getHealth,
  runHealth,
  type HealthCheck,
  type HealthReport,
  type HealthStatus,
} from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { type Key, formatDateTime, t } from "@/i18n"

type State =
  { status: "loading" } | { status: "failed" } | { status: "ready"; report: HealthReport }

const label = {
  fail: "health.failed",
  warn: "health.warning",
  ok: "health.ok",
} as const satisfies Record<HealthStatus, Key>
const rank: Record<HealthStatus, number> = { fail: 0, warn: 1, ok: 2 }

/** Problems first, in the order the server listed them, then what is fine. */
function sorted(checks: HealthCheck[]): HealthCheck[] {
  return [...checks].sort((a, b) => rank[a.status] - rank[b.status])
}

/** The checks of "netscribe doctor" that a service without root can make about itself. */
export function Health() {
  const [state, setState] = useState<State>({ status: "loading" })
  const [running, setRunning] = useState(false)
  const [note, setNote] = useState("")

  useEffect(() => {
    let live = true
    getHealth()
      .then((report) => live && setState({ status: "ready", report }))
      .catch(() => live && setState({ status: "failed" }))
    return () => {
      live = false
    }
  }, [])

  const again = useCallback(async () => {
    setRunning(true)
    setNote("")
    try {
      setState({ status: "ready", report: await runHealth() })
    } catch (e) {
      setNote(
        e instanceof ApiError && e.status === 429
          ? t("health.ranRecently")
          : t("health.couldNotRun"),
      )
    } finally {
      setRunning(false)
    }
  }, [])

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold">{t("health.health")}</h1>
        <Button
          variant="outline"
          disabled={running || state.status !== "ready"}
          onClick={() => void again()}
        >
          {t("health.runAgain")}
        </Button>
      </div>
      {note ? <Alert>{note}</Alert> : null}
      <Body state={state} />
    </div>
  )
}

function Body({ state }: { state: State }) {
  if (state.status === "loading") {
    return (
      <div role="status" aria-label={t("health.loadingHealth")} className="flex flex-col gap-2">
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-16 w-full" />
      </div>
    )
  }
  if (state.status === "failed") {
    return <Alert variant="destructive">{t("health.theHealthChecksCouldNot")}</Alert>
  }
  const { report } = state
  const problems = report.checks.filter((c) => c.status !== "ok").length
  return (
    <>
      <p className="text-sm text-muted-foreground">
        {problems === 0 ? t("health.allFine") : t("health.needLook", { n: problems })}{" "}
        {t("health.lastRun", { date: formatDateTime(report.ranAt) })}
      </p>
      <ul className="flex flex-col gap-3">
        {sorted(report.checks).map((c) => (
          <li key={c.id} className="rounded-lg border p-4">
            <div className="flex flex-wrap items-center gap-3">
              <Badge variant={c.status === "ok" ? "outline" : "default"}>
                {t(label[c.status])}
              </Badge>
              <span className="font-medium">{c.title}</span>
            </div>
            {c.detail ? <p className="mt-2 text-sm text-muted-foreground">{c.detail}</p> : null}
            {c.fix && c.status !== "ok" ? (
              <p className="mt-2 text-sm">
                <span className="font-medium">{t("health.whatToDo")} </span>
                <code className="font-mono text-xs">{c.fix}</code>
              </p>
            ) : null}
          </li>
        ))}
      </ul>
    </>
  )
}

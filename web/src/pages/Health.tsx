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

type State =
  { status: "loading" } | { status: "failed" } | { status: "ready"; report: HealthReport }

const label: Record<HealthStatus, string> = { fail: "Failed", warn: "Warning", ok: "OK" }
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
          ? "The checks ran a moment ago. Wait a few seconds."
          : "The checks could not be run.",
      )
    } finally {
      setRunning(false)
    }
  }, [])

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold">Health</h1>
        <Button
          variant="outline"
          disabled={running || state.status !== "ready"}
          onClick={() => void again()}
        >
          Run again
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
      <div role="status" aria-label="Loading health" className="flex flex-col gap-2">
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-16 w-full" />
      </div>
    )
  }
  if (state.status === "failed") {
    return (
      <Alert variant="destructive">The health checks could not be loaded. Try again later.</Alert>
    )
  }
  const { report } = state
  const problems = report.checks.filter((c) => c.status !== "ok").length
  return (
    <>
      <p className="text-sm text-muted-foreground">
        {problems === 0
          ? "Every check is fine."
          : `${problems} ${problems === 1 ? "check needs" : "checks need"} a look.`}{" "}
        Last run {new Date(report.ranAt).toLocaleString()}. The checks only read: nothing is
        changed.
      </p>
      <ul className="flex flex-col gap-3">
        {sorted(report.checks).map((c) => (
          <li key={c.id} className="rounded-lg border p-4">
            <div className="flex flex-wrap items-center gap-3">
              <Badge variant={c.status === "ok" ? "outline" : "default"}>{label[c.status]}</Badge>
              <span className="font-medium">{c.title}</span>
            </div>
            {c.detail ? <p className="mt-2 text-sm text-muted-foreground">{c.detail}</p> : null}
            {c.fix && c.status !== "ok" ? (
              <p className="mt-2 text-sm">
                <span className="font-medium">What to do: </span>
                <code className="font-mono text-xs">{c.fix}</code>
              </p>
            ) : null}
          </li>
        ))}
      </ul>
    </>
  )
}

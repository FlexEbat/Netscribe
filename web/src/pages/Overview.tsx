import { useEffect, useState } from "react"
import { Link } from "react-router-dom"
import { getOverview, type Overview as OverviewData } from "@/api/client"
import { openEvents } from "@/api/events"
import { EmptyState } from "@/components/EmptyState"
import { Alert } from "@/components/ui/alert"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { kindClasses, kindLabels } from "@/lib/changes"
import { cn } from "@/lib/utils"

type State = { status: "loading" } | { status: "failed" } | { status: "ready"; data: OverviewData }

function when(iso: string): string {
  const t = new Date(iso)
  return Number.isNaN(t.getTime()) ? "–" : t.toLocaleString()
}

/** The state of the network on one page. Every tile leads to the page that has the details. */
export function Overview() {
  const [state, setState] = useState<State>({ status: "loading" })
  const [refresh, setRefresh] = useState(0)

  useEffect(() => {
    let cancelled = false
    getOverview()
      .then((data) => !cancelled && setState({ status: "ready", data }))
      .catch(() => !cancelled && setState({ status: "failed" }))
    return () => {
      cancelled = true
    }
  }, [refresh])

  useEffect(
    () =>
      openEvents({
        "scan.finished": () => setRefresh((n) => n + 1),
        "topology.changed": () => setRefresh((n) => n + 1),
      }),
    [],
  )

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-semibold">Overview</h1>
      <Body state={state} />
    </div>
  )
}

function Body({ state }: { state: State }) {
  if (state.status === "loading") {
    return (
      <div role="status" aria-label="Loading the overview" className="grid gap-3 sm:grid-cols-3">
        <Skeleton className="h-20" />
        <Skeleton className="h-20" />
        <Skeleton className="h-20" />
      </div>
    )
  }
  if (state.status === "failed") {
    return <Alert variant="destructive">The overview could not be loaded. Try again later.</Alert>
  }
  const { devices, risky, lastScan, recentScans, changes } = state.data
  if (lastScan === null && devices.total === 0) {
    return (
      <EmptyState
        title="Nothing to show yet"
        text="Run a scan on the inventory page. The overview fills in after the first one."
      />
    )
  }
  const tiles: { label: string; value: number; to: string; alert?: boolean }[] = [
    { label: "Devices", value: devices.total, to: "/inventory" },
    { label: "Online", value: devices.online, to: "/inventory?status=online" },
    { label: "Offline", value: devices.offline, to: "/inventory?status=offline" },
    {
      label: "Unknown",
      value: devices.unknown,
      to: "/inventory?trust=unknown",
      alert: devices.unknown > 0,
    },
    { label: "New this week", value: devices.newThisWeek, to: "/changes" },
    { label: "Risky services", value: risky, to: "/services", alert: risky > 0 },
  ]
  return (
    <>
      <ul className="grid gap-3 sm:grid-cols-3 lg:grid-cols-6">
        {tiles.map((t) => (
          <li key={t.label}>
            <Link
              to={t.to}
              className="flex flex-col gap-1 rounded-lg border bg-card p-3 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
            >
              <span
                className={cn(
                  "text-2xl font-semibold",
                  t.alert && "text-amber-700 dark:text-amber-300",
                )}
              >
                {t.value}
              </span>
              <span className="text-sm text-muted-foreground">{t.label}</span>
            </Link>
          </li>
        ))}
      </ul>

      <Card>
        <CardHeader>
          <CardTitle>Scans</CardTitle>
          <CardDescription>
            {lastScan === null
              ? "No scan has run."
              : `The last scan ${lastScan.status === "running" ? "started" : lastScan.status === "failed" ? "failed" : "finished"} ${when(lastScan.finishedAt ?? lastScan.startedAt)}.`}
          </CardDescription>
        </CardHeader>
        <CardContent className="text-sm">
          {recentScans.total === 0
            ? "No scan has finished yet."
            : recentScans.failed === 0
              ? `All of the last ${recentScans.total} scans finished.`
              : `${recentScans.failed} of the last ${recentScans.total} scans failed.`}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Latest changes</CardTitle>
        </CardHeader>
        <CardContent>
          {changes.length === 0 ? (
            <p className="text-sm text-muted-foreground">No change has been recorded.</p>
          ) : (
            <ul className="flex flex-col gap-2 text-sm">
              {changes.map((c) => (
                <li key={c.id} className="flex flex-wrap items-center gap-2">
                  <span
                    className={cn("rounded-md border px-2 py-0.5 text-xs", kindClasses(c.kind))}
                  >
                    {kindLabels[c.kind] ?? c.kind}
                  </span>
                  <span>{c.summary}</span>
                  <span className="text-muted-foreground">{when(c.at)}</span>
                </li>
              ))}
            </ul>
          )}
          <Link to="/changes" className="mt-3 inline-block text-sm underline underline-offset-4">
            All changes
          </Link>
        </CardContent>
      </Card>
    </>
  )
}

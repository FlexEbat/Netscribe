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
import { formatDateTime, t } from "@/i18n"
import { PageTitle } from "@/components/PageTitle"

type State = { status: "loading" } | { status: "failed" } | { status: "ready"; data: OverviewData }

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
    <div className="flex flex-col gap-8">
      <PageTitle page="overview">{t("overview.overview")}</PageTitle>
      <Body state={state} />
    </div>
  )
}

function Body({ state }: { state: State }) {
  if (state.status === "loading") {
    return (
      <div
        role="status"
        aria-label={t("overview.loadingTheOverview")}
        className="grid gap-3 sm:grid-cols-3"
      >
        <Skeleton className="h-20" />
        <Skeleton className="h-20" />
        <Skeleton className="h-20" />
      </div>
    )
  }
  if (state.status === "failed") {
    return <Alert variant="destructive">{t("overview.theOverviewCouldNotBe")}</Alert>
  }
  const { devices, risky, lastScan, recentScans, changes } = state.data
  if (lastScan === null && devices.total === 0) {
    return <EmptyState title={t("overview.nothingToShowYet")} text={t("overview.nothingText")} />
  }
  const tiles: { label: string; value: number; to: string; alert?: boolean }[] = [
    { label: t("overview.devices"), value: devices.total, to: "/inventory" },
    { label: t("overview.online"), value: devices.online, to: "/inventory?status=online" },
    { label: t("overview.offline"), value: devices.offline, to: "/inventory?status=offline" },
    {
      label: t("overview.unknown"),
      value: devices.unknown,
      to: "/inventory?trust=unknown",
      alert: devices.unknown > 0,
    },
    { label: t("overview.newThisWeek"), value: devices.newThisWeek, to: "/changes" },
    { label: t("overview.riskyServices"), value: risky, to: "/services", alert: risky > 0 },
  ]
  return (
    <>
      <ul className="surface grid grid-cols-2 divide-x divide-y sm:grid-cols-3 lg:grid-cols-6 lg:divide-y-0">
        {tiles.map((t) => (
          <li key={t.label} className="group">
            <Link
              to={t.to}
              className="flex h-full flex-col gap-2 p-4 transition-colors hover:bg-accent/50 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
            >
              <span className="eyebrow text-muted-foreground">{t.label}</span>
              <span
                className={cn(
                  "display text-5xl leading-none font-medium transition-transform group-hover:translate-x-1",
                  t.alert && "text-warn",
                )}
              >
                {t.value}
              </span>
            </Link>
          </li>
        ))}
      </ul>

      <Card>
        <CardHeader>
          <CardTitle>{t("overview.scans")}</CardTitle>
          <CardDescription>
            {lastScan === null
              ? t("overview.noScan")
              : t(
                  lastScan.status === "running"
                    ? "overview.lastStarted"
                    : lastScan.status === "failed"
                      ? "overview.lastFailed"
                      : "overview.lastFinished",
                  { when: formatDateTime(lastScan.finishedAt ?? lastScan.startedAt) },
                )}
          </CardDescription>
        </CardHeader>
        <CardContent className="text-sm">
          {recentScans.total === 0
            ? t("overview.noneFinished")
            : recentScans.failed === 0
              ? t("overview.allFinished", { n: recentScans.total })
              : t("overview.someFailed", {
                  failed: recentScans.failed,
                  n: recentScans.total,
                })}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("overview.latestChanges")}</CardTitle>
        </CardHeader>
        <CardContent>
          {changes.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("overview.noChangeHasBeenRecorded")}</p>
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
                  <span className="text-muted-foreground">{formatDateTime(c.at)}</span>
                </li>
              ))}
            </ul>
          )}
          <Link to="/changes" className="mt-3 inline-block text-sm underline underline-offset-4">
            {t("overview.allChanges")}
          </Link>
        </CardContent>
      </Card>
    </>
  )
}

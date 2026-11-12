import { useCallback, useEffect, useState } from "react"
import {
  CartesianGrid,
  Area,
  AreaChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts"
import { listChanges, listScans, type Change, type Scan } from "@/api/client"
import { openEvents } from "@/api/events"
import { EmptyState } from "@/components/EmptyState"
import { Alert } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { kindClasses, kindLabels, scanPoints } from "@/lib/changes"
import { formatDateTime, t } from "@/i18n"
import { PageTitle } from "@/components/PageTitle"

type State =
  | { status: "loading" }
  | { status: "failed" }
  | { status: "ready"; changes: Change[]; scans: Scan[] }

export function Changes() {
  const [state, setState] = useState<State>({ status: "loading" })

  const load = useCallback(() => {
    Promise.all([listChanges(), listScans()])
      .then(([changes, scans]) => setState({ status: "ready", changes, scans }))
      .catch(() => setState((s) => (s.status === "ready" ? s : { status: "failed" })))
  }, [])

  // A finished scan is what adds a change and a point on the chart.
  useEffect(() => {
    load()
    return openEvents({ "scan.finished": load })
  }, [load])

  return (
    <div className="flex flex-col gap-6">
      <PageTitle page="changes">{t("changes.changes")}</PageTitle>
      <Body state={state} />
    </div>
  )
}

function Body({ state }: { state: State }) {
  if (state.status === "loading") {
    return (
      <div role="status" aria-label={t("changes.loadingChanges")} className="flex flex-col gap-2">
        <Skeleton className="h-48 w-full" />
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
      </div>
    )
  }
  if (state.status === "failed") {
    return <Alert variant="destructive">{t("changes.theChangesCouldNotBe")}</Alert>
  }
  return (
    <>
      <DeviceChart scans={state.scans} />
      <Feed changes={state.changes} />
    </>
  )
}

function DeviceChart({ scans }: { scans: Scan[] }) {
  const points = scanPoints(scans)
  if (points.length === 0) {
    return <p className="text-sm text-muted-foreground">{t("changes.theChartOfDevicesPer")}</p>
  }
  const last = points[points.length - 1]
  return (
    <figure className="flex flex-col gap-2" aria-label={t("changes.devicesPerScan")}>
      <figcaption className="text-sm text-muted-foreground">
        {t("changes.chartCaption", { devices: last?.devices ?? 0, scan: last?.scanId ?? 0 })}
      </figcaption>
      <div className="h-60 w-full">
        <ResponsiveContainer
          width="100%"
          height="100%"
          initialDimension={{ width: 640, height: 240 }}
        >
          <AreaChart data={points} margin={{ top: 8, right: 16, bottom: 8, left: 0 }}>
            <defs>
              <linearGradient id="devices-fill" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="var(--primary)" stopOpacity={0.45} />
                <stop offset="100%" stopColor="var(--primary)" stopOpacity={0} />
              </linearGradient>
            </defs>
            <CartesianGrid vertical={false} stroke="var(--border)" strokeDasharray="2 4" />
            <XAxis
              dataKey="label"
              tick={{ fontSize: 11, fontFamily: "var(--font-mono)" }}
              stroke="var(--border)"
            />
            <YAxis
              allowDecimals={false}
              width={32}
              tick={{ fontSize: 11, fontFamily: "var(--font-mono)" }}
              stroke="var(--border)"
            />
            <Tooltip
              contentStyle={{
                background: "var(--popover)",
                border: "1px solid var(--border)",
                borderRadius: 12,
                fontFamily: "var(--font-mono)",
                fontSize: 12,
              }}
            />
            <Area
              type="monotone"
              dataKey="devices"
              name="Devices"
              stroke="var(--brand)"
              strokeWidth={2.5}
              fill="url(#devices-fill)"
              dot={points.length < 40}
              isAnimationActive={false}
            />
          </AreaChart>
        </ResponsiveContainer>
      </div>
    </figure>
  )
}

function Feed({ changes }: { changes: Change[] }) {
  if (changes.length === 0) {
    return <EmptyState title={t("changes.noChangesYet")} text={t("changes.noChangesText")} />
  }
  return (
    <ul aria-label={t("changes.changes")} className="flex flex-col divide-y rounded-md border">
      {changes.map((c) => (
        <li key={c.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-sm">
          <time dateTime={c.at} className="w-44 shrink-0 text-xs text-muted-foreground">
            {formatDateTime(c.at)}
          </time>
          <span
            className={`w-36 shrink-0 rounded border px-2 py-0.5 text-center text-xs font-medium ${kindClasses(c.kind)}`}
          >
            {kindLabels[c.kind] ?? c.kind}
          </span>
          <span className="min-w-0 flex-1 break-words">{c.summary}</span>
        </li>
      ))}
    </ul>
  )
}

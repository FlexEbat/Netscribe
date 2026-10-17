import { useCallback, useEffect, useState } from "react"
import {
  CartesianGrid,
  Line,
  LineChart,
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
      <h1 className="text-lg font-semibold">Changes</h1>
      <Body state={state} />
    </div>
  )
}

function Body({ state }: { state: State }) {
  if (state.status === "loading") {
    return (
      <div role="status" aria-label="Loading changes" className="flex flex-col gap-2">
        <Skeleton className="h-48 w-full" />
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
      </div>
    )
  }
  if (state.status === "failed") {
    return <Alert variant="destructive">The changes could not be loaded. Try again later.</Alert>
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
    return (
      <p className="text-sm text-muted-foreground">
        The chart of devices per scan appears after the first completed scan.
      </p>
    )
  }
  const last = points[points.length - 1]
  return (
    <figure className="flex flex-col gap-2" aria-label="Devices per scan">
      <figcaption className="text-sm text-muted-foreground">
        Devices per completed scan. Last: {last?.devices} in scan #{last?.scanId}.
      </figcaption>
      <div className="h-60 w-full">
        <ResponsiveContainer
          width="100%"
          height="100%"
          initialDimension={{ width: 640, height: 240 }}
        >
          <LineChart data={points} margin={{ top: 8, right: 16, bottom: 8, left: 0 }}>
            <CartesianGrid strokeDasharray="3 3" />
            <XAxis dataKey="label" tick={{ fontSize: 11 }} />
            <YAxis allowDecimals={false} width={32} tick={{ fontSize: 11 }} />
            <Tooltip />
            <Line
              type="monotone"
              dataKey="devices"
              name="Devices"
              stroke="var(--color-primary, #2563eb)"
              strokeWidth={2}
              dot={points.length < 40}
              isAnimationActive={false}
            />
          </LineChart>
        </ResponsiveContainer>
      </div>
    </figure>
  )
}

function Feed({ changes }: { changes: Change[] }) {
  if (changes.length === 0) {
    return (
      <EmptyState
        title="No changes yet"
        text="Changes appear here when a scan finds something new, lost or different. The first scan only sets the baseline."
      />
    )
  }
  return (
    <ul aria-label="Changes" className="flex flex-col divide-y rounded-md border">
      {changes.map((c) => (
        <li key={c.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-sm">
          <time dateTime={c.at} className="w-44 shrink-0 text-xs text-muted-foreground">
            {new Date(c.at).toLocaleString()}
          </time>
          <span
            className={`w-36 shrink-0 rounded border px-1.5 py-0.5 text-center text-xs font-medium ${kindClasses(c.kind)}`}
          >
            {kindLabels[c.kind] ?? c.kind}
          </span>
          <span className="min-w-0 flex-1 break-words">{c.summary}</span>
        </li>
      ))}
    </ul>
  )
}

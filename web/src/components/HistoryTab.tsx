import { useEffect, useState } from "react"
import { getDeviceHistory, type DeviceHistory } from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"

type State =
  { status: "loading" } | { status: "failed" } | { status: "ready"; history: DeviceHistory }

function time(iso: string): string {
  const t = new Date(iso)
  return Number.isNaN(t.getTime()) ? "–" : t.toLocaleString()
}

/** How often the stored scans saw the device, where it lived and when it was away. */
export function HistoryTab({ deviceId }: { deviceId: number }) {
  const [state, setState] = useState<State>({ status: "loading" })

  useEffect(() => {
    let cancelled = false
    getDeviceHistory(deviceId)
      .then((history) => !cancelled && setState({ status: "ready", history }))
      .catch(() => !cancelled && setState({ status: "failed" }))
    return () => {
      cancelled = true
    }
  }, [deviceId])

  if (state.status === "loading") {
    return <Skeleton role="status" aria-label="Loading the history" className="h-24 w-full" />
  }
  if (state.status === "failed") {
    return <Alert variant="destructive">The history could not be loaded.</Alert>
  }
  const h = state.history
  if (h.scans === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        No scan has seen this device since the history began. Devices added by hand are not scanned,
        so they have none.
      </p>
    )
  }
  return (
    <div className="flex flex-col gap-4 text-sm">
      <p>
        <span className="text-lg font-semibold">{h.uptime}%</span> uptime, seen in {h.online} of{" "}
        {h.scans} stored {h.scans === 1 ? "scan" : "scans"}. The history starts when it was first
        recorded.
      </p>
      <section aria-label="Addresses">
        <h3 className="mb-1 font-medium">Addresses</h3>
        <ul className="flex flex-col gap-1">
          {h.ips.map((a) => (
            <li key={`${a.ip}-${a.from}`}>
              <span className="font-mono">{a.ip}</span>{" "}
              <span className="text-muted-foreground">
                {time(a.from)}
                {a.to !== a.from ? ` to ${time(a.to)}` : ""}
              </span>
            </li>
          ))}
        </ul>
      </section>
      <section aria-label="Outages">
        <h3 className="mb-1 font-medium">Outages</h3>
        {h.outages.length === 0 ? (
          <p className="text-muted-foreground">No scan has missed this device.</p>
        ) : (
          <ul className="flex flex-col gap-1">
            {h.outages.map((o) => (
              <li key={o.from}>
                {o.to === null ? (
                  <>
                    Away since {time(o.from)}, <span className="font-medium">still away</span>
                  </>
                ) : (
                  <>
                    Away from {time(o.from)} until {time(o.to)}
                  </>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}

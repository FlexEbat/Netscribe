import { useEffect, useState } from "react"
import { listDevices, type Device } from "@/api/devices"
import { EmptyState } from "@/components/EmptyState"
import { Alert } from "@/components/ui/alert"

type State = { status: "loading" } | { status: "failed" } | { status: "ready"; devices: Device[] }

export function Topology() {
  const [state, setState] = useState<State>({ status: "loading" })

  useEffect(() => {
    let cancelled = false
    listDevices()
      .then((devices) => {
        if (!cancelled) setState({ status: "ready", devices })
      })
      .catch(() => {
        if (!cancelled) setState({ status: "failed" })
      })
    return () => {
      cancelled = true
    }
  }, [])

  if (state.status === "loading") {
    return (
      <p role="status" className="text-sm text-muted-foreground">
        Loading devices…
      </p>
    )
  }
  if (state.status === "failed") {
    return <Alert variant="destructive">The devices could not be loaded. Try again later.</Alert>
  }
  if (state.devices.length === 0) {
    return (
      <EmptyState
        title="No scans yet"
        text="Run a scan to discover the devices on your network. The topology appears here."
      />
    )
  }
  return <DeviceTable devices={state.devices} />
}

function DeviceTable({ devices }: { devices: Device[] }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-sm">
        <caption className="mb-3 text-left text-lg font-semibold">
          Devices ({devices.length})
        </caption>
        <thead className="border-b text-muted-foreground">
          <tr>
            <th scope="col" className="py-2 pr-4 font-medium">
              IP
            </th>
            <th scope="col" className="py-2 pr-4 font-medium">
              MAC
            </th>
            <th scope="col" className="py-2 pr-4 font-medium">
              Hostname
            </th>
            <th scope="col" className="py-2 pr-4 font-medium">
              Kind
            </th>
            <th scope="col" className="py-2 font-medium">
              Status
            </th>
          </tr>
        </thead>
        <tbody>
          {devices.map((d) => (
            <tr key={d.id} className="border-b last:border-0">
              <td className="py-2 pr-4 font-mono">{d.ip || "–"}</td>
              <td className="py-2 pr-4 font-mono">{d.mac || "–"}</td>
              <td className="py-2 pr-4">{d.hostname || "–"}</td>
              <td className="py-2 pr-4 capitalize">{d.kind}</td>
              <td className="py-2">{d.online ? "Online" : "Offline"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

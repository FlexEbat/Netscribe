import { useCallback, useEffect, useState } from "react"
import { useSearchParams } from "react-router-dom"
import { getTopology, moveDevice, type Topology as TopologyData } from "@/api/client"
import { openEvents } from "@/api/events"
import { useSession } from "@/api/session"
import { DevicePanel } from "@/components/DevicePanel"
import { EmptyState } from "@/components/EmptyState"
import { TopologyCanvas } from "@/components/TopologyCanvas"
import { Alert } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { can } from "@/lib/permissions"

type State =
  { status: "loading" } | { status: "failed" } | { status: "ready"; topology: TopologyData }

export function Topology() {
  const { state: session } = useSession()
  const canMove = session.status === "signedIn" && can(session.session.permissions, "devices:write")
  const [params, setParams] = useSearchParams()
  const [state, setState] = useState<State>({ status: "loading" })
  const [saveFailed, setSaveFailed] = useState(false)

  const load = useCallback(() => {
    getTopology()
      .then((topology) => setState({ status: "ready", topology }))
      .catch(() => setState((s) => (s.status === "ready" ? s : { status: "failed" })))
  }, [])

  useEffect(() => {
    load()
    return openEvents({ "topology.changed": load })
  }, [load])

  const select = useCallback(
    (id: number | null) => {
      setParams(
        (p) => {
          const next = new URLSearchParams(p)
          if (id === null) next.delete("device")
          else next.set("device", String(id))
          return next
        },
        { replace: true },
      )
    },
    [setParams],
  )

  const move = useCallback((id: number, x: number, y: number) => {
    setSaveFailed(false)
    moveDevice(id, x, y).catch(() => setSaveFailed(true))
  }, [])

  const selected = Number(params.get("device"))
  const deviceId = Number.isInteger(selected) && selected > 0 ? selected : null

  if (state.status === "loading") {
    return <Skeleton role="status" aria-label="Loading topology" className="h-96 w-full" />
  }
  if (state.status === "failed") {
    return <Alert variant="destructive">The topology could not be loaded. Try again later.</Alert>
  }
  if (state.topology.devices.length === 0) {
    return (
      <EmptyState
        title="No scans yet"
        text="Run a scan to discover the devices on your network. The topology appears here."
      />
    )
  }
  return (
    <div className="flex flex-col gap-3">
      {saveFailed ? <Alert variant="destructive">The position could not be saved.</Alert> : null}
      <TopologyCanvas
        topology={state.topology}
        onMove={move}
        onSelect={select}
        readOnly={!canMove}
      />
      {deviceId !== null ? <DevicePanel deviceId={deviceId} onClose={() => select(null)} /> : null}
    </div>
  )
}

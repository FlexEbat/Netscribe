import { useCallback, useEffect, useState } from "react"
import { useSearchParams } from "react-router-dom"
import { getTopology, moveDevice, type Topology as TopologyData } from "@/api/client"
import { openEvents } from "@/api/events"
import { useSession } from "@/api/session"
import { AddDeviceDialog } from "@/components/AddDeviceDialog"
import { ContainerPanel } from "@/components/ContainerPanel"
import { ExportMenu } from "@/components/ExportMenu"
import { DevicePanel } from "@/components/DevicePanel"
import { EmptyState } from "@/components/EmptyState"
import { LinkBar } from "@/components/LinkBar"
import { PortsDialog } from "@/components/PortsDialog"
import { TopologyCanvas } from "@/components/TopologyCanvas"
import { Alert } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { can } from "@/lib/permissions"
import { displayName } from "@/lib/topology"

type State =
  { status: "loading" } | { status: "failed" } | { status: "ready"; topology: TopologyData }

export function Topology() {
  const { state: session } = useSession()
  const canMove = session.status === "signedIn" && can(session.session.permissions, "devices:write")
  const canExport = session.status === "signedIn" && can(session.session.permissions, "export:run")
  const [params, setParams] = useSearchParams()
  const [state, setState] = useState<State>({ status: "loading" })
  const [saveFailed, setSaveFailed] = useState(false)
  const [connecting, setConnecting] = useState<{ a: number; b: number } | null>(null)
  const [linkId, setLinkId] = useState<number | null>(null)

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
          else {
            next.set("device", String(id))
            next.delete("container")
          }
          return next
        },
        { replace: true },
      )
    },
    [setParams],
  )

  const selectContainer = useCallback(
    (id: string | null) => {
      setParams(
        (p) => {
          const next = new URLSearchParams(p)
          if (id === null) next.delete("container")
          else {
            next.set("container", id)
            next.delete("device")
          }
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
  const toolbar =
    canMove || canExport ? (
      <div className="flex justify-end gap-2">
        {canExport ? <ExportMenu formats={["md", "drawio"]} /> : null}
        {canMove ? <AddDeviceDialog onCreated={load} /> : null}
      </div>
    ) : null
  if (state.topology.devices.length === 0) {
    return (
      <div className="flex flex-col gap-3">
        {toolbar}
        <EmptyState
          title="No scans yet"
          text="Run a scan to discover the devices on your network. The topology appears here."
        />
      </div>
    )
  }
  const devices = state.topology.devices
  const nameOf = (id: number) => {
    const d = devices.find((x) => x.id === id)
    return d ? displayName(d) : `device ${id}`
  }
  const link = state.topology.links.find((l) => l.id === linkId)
  // A container that is gone from the topology closes its panel.
  const container = state.topology.containers.find((c) => c.id === params.get("container"))
  return (
    <div className="flex flex-col gap-3">
      {toolbar}
      {saveFailed ? <Alert variant="destructive">The position could not be saved.</Alert> : null}
      {link ? (
        <LinkBar
          key={link.id}
          link={link}
          aName={nameOf(link.aDeviceId)}
          bName={nameOf(link.bDeviceId)}
          canEdit={canMove}
          onDeleted={() => {
            setLinkId(null)
            load()
          }}
        />
      ) : null}
      <TopologyCanvas
        topology={state.topology}
        onMove={move}
        onSelect={select}
        onSelectContainer={selectContainer}
        onConnectNodes={(a, b) => setConnecting({ a, b })}
        onSelectLink={setLinkId}
        readOnly={!canMove}
      />
      {connecting ? (
        <PortsDialog
          aDeviceId={connecting.a}
          aName={nameOf(connecting.a)}
          bDeviceId={connecting.b}
          bName={nameOf(connecting.b)}
          onCancel={() => setConnecting(null)}
          onDone={() => {
            setConnecting(null)
            load()
          }}
        />
      ) : null}
      {container ? (
        <ContainerPanel
          container={container}
          hostName={nameOf(container.deviceId)}
          onClose={() => selectContainer(null)}
        />
      ) : null}
      {deviceId !== null ? (
        <DevicePanel
          deviceId={deviceId}
          onClose={() => select(null)}
          canEdit={canMove}
          onChanged={load}
        />
      ) : null}
    </div>
  )
}

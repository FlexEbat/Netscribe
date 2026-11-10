import { useCallback, useEffect, useState } from "react"
import { useSearchParams } from "react-router-dom"
import { getTopology, moveDevice, type Topology as TopologyData } from "@/api/client"
import { openEvents } from "@/api/events"
import { useSession } from "@/api/session"
import { AddDeviceDialog } from "@/components/AddDeviceDialog"
import { ClusterCanvas } from "@/components/ClusterCanvas"
import { ClusterControls, type TopologyView } from "@/components/ClusterControls"
import { ContainerPanel } from "@/components/ContainerPanel"
import { ExportMenu } from "@/components/ExportMenu"
import { DevicePanel } from "@/components/DevicePanel"
import { EmptyState } from "@/components/EmptyState"
import { LinkBar } from "@/components/LinkBar"
import { PortsDialog } from "@/components/PortsDialog"
import { TopologyCanvas } from "@/components/TopologyCanvas"
import { Alert } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { CLUSTER_AUTO_ABOVE, clusterKeyOf, defaultBy, type ClusterBy } from "@/lib/clusters"
import { can } from "@/lib/permissions"
import { displayName } from "@/lib/topology"
import { t } from "@/i18n"

const VIEW_KEY = "netscribe.topology.view"

/** The view a person chose last time, or null when they never chose. */
function savedView(): TopologyView | null {
  try {
    const v = window.localStorage.getItem(VIEW_KEY)
    return v === "devices" || v === "clusters" ? v : null
  } catch {
    return null
  }
}

type State =
  { status: "loading" } | { status: "failed" } | { status: "ready"; topology: TopologyData }

export function Topology() {
  const { state: session } = useSession()
  const canMove = session.status === "signedIn" && can(session.session.permissions, "devices:write")
  const canRun = session.status === "signedIn" && can(session.session.permissions, "scans:run")
  const canExport = session.status === "signedIn" && can(session.session.permissions, "export:run")
  const [params, setParams] = useSearchParams()
  const [state, setState] = useState<State>({ status: "loading" })
  const [saveFailed, setSaveFailed] = useState(false)
  const [connecting, setConnecting] = useState<{ a: number; b: number } | null>(null)
  const [linkId, setLinkId] = useState<number | null>(null)
  // null means the page decides: clusters for a big network, devices for a small one.
  const [chosenView, setChosenView] = useState<TopologyView | null>(savedView)
  const [chosenBy, setChosenBy] = useState<ClusterBy | null>(null)
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set())
  const [focus, setFocus] = useState<string | null>(null)
  const [seen, setSeen] = useState<number | null>(null)

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

  const allDevices = state.status === "ready" ? state.topology.devices : null
  const view: TopologyView =
    chosenView ?? (allDevices && allDevices.length > CLUSTER_AUTO_ABOVE ? "clusters" : "devices")
  const by: ClusterBy = chosenBy ?? defaultBy(allDevices ?? [])

  const toggleCluster = useCallback((key: string) => {
    setExpanded((cur) => {
      const next = new Set(cur)
      if (next.has(key)) next.delete(key)
      else {
        next.add(key)
        setFocus(key)
      }
      return next
    })
  }, [])

  const chooseView = useCallback((v: TopologyView) => {
    setChosenView(v)
    try {
      window.localStorage.setItem(VIEW_KEY, v)
    } catch {
      // A browser that keeps nothing still shows the view that was chosen.
    }
  }, [])

  const chooseBy = useCallback((b: ClusterBy) => {
    setChosenBy(b)
    setExpanded(new Set())
    setFocus(null)
  }, [])

  // A device that is selected (from a link, say) must be on the screen: its cluster opens.
  // This is state adjusted while rendering, the way React asks for a change that follows a
  // prop, so the open cluster is there in the same frame.
  if (deviceId === null) {
    if (seen !== null) setSeen(null)
  } else if (view === "clusters" && allDevices && seen !== deviceId) {
    const d = allDevices.find((x) => x.id === deviceId)
    if (d) {
      const key = clusterKeyOf(d, by)
      setSeen(deviceId)
      setExpanded((cur) => (cur.has(key) ? cur : new Set(cur).add(key)))
      setFocus(key)
    }
  }

  if (state.status === "loading") {
    return (
      <Skeleton role="status" aria-label={t("topology.loadingTopology")} className="h-96 w-full" />
    )
  }
  if (state.status === "failed") {
    return <Alert variant="destructive">{t("topology.theTopologyCouldNotBe")}</Alert>
  }
  const actions =
    canMove || canExport ? (
      <div className="flex justify-end gap-2">
        {canExport ? <ExportMenu formats={["md", "html", "pdf", "drawio"]} /> : null}
        {canMove ? <AddDeviceDialog onCreated={load} /> : null}
      </div>
    ) : null
  const toolbar = actions
  if (state.topology.devices.length === 0) {
    return (
      <div className="flex flex-col gap-3">
        {toolbar}
        <EmptyState title={t("topology.noScansYet")} text={t("topology.noScansText")} />
      </div>
    )
  }
  const devices = state.topology.devices
  const nameOf = (id: number) => {
    const d = devices.find((x) => x.id === id)
    return d ? displayName(d) : t("topology.deviceId", { id })
  }
  const link = state.topology.links.find((l) => l.id === linkId)
  // A container that is gone from the topology closes its panel.
  const container = state.topology.containers.find((c) => c.id === params.get("container"))
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <ClusterControls
          view={view}
          onView={chooseView}
          by={by}
          onBy={chooseBy}
          openCount={expanded.size}
          onCollapseAll={() => setExpanded(new Set())}
        />
        {actions}
      </div>
      {saveFailed ? (
        <Alert variant="destructive">{t("topology.thePositionCouldNotBe")}</Alert>
      ) : null}
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
      {view === "clusters" ? (
        <ClusterCanvas
          topology={state.topology}
          by={by}
          expanded={expanded}
          onToggle={toggleCluster}
          selectedDevice={deviceId}
          onSelect={select}
          onSelectLink={setLinkId}
          focus={focus}
        />
      ) : (
        <TopologyCanvas
          topology={state.topology}
          onMove={move}
          onSelect={select}
          onSelectContainer={selectContainer}
          onConnectNodes={(a, b) => setConnecting({ a, b })}
          onSelectLink={setLinkId}
          readOnly={!canMove}
        />
      )}
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
          canIdentify={canRun}
          onChanged={load}
        />
      ) : null}
    </div>
  )
}

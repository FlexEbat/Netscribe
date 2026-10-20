import { useEffect, useState } from "react"
import { Pencil, ScanSearch } from "lucide-react"
import {
  ApiError,
  deleteDevice,
  getDeviceDetail,
  getNmap,
  identifyDevice,
  updateDevice,
  type Device,
  type DeviceDetail,
} from "@/api/client"
import { openEvents } from "@/api/events"
import { DeleteDeviceButton } from "@/components/DeleteDeviceButton"
import { DeviceForm } from "@/components/DeviceForm"
import { InterfacesTable } from "@/components/InterfacesTable"
import { ServicesTable } from "@/components/ServicesTable"
import { StatusBadge } from "@/components/StatusBadge"
import { Button } from "@/components/ui/button"
import { Alert } from "@/components/ui/alert"
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { displayName, kindLabels } from "@/lib/topology"

interface DevicePanelProps {
  deviceId: number
  onClose: () => void
  /** The user may edit and delete devices. */
  canEdit?: boolean
  /** Called after a device was changed or deleted, so the canvas can load the new state. */
  onChanged?: () => void
  /** The user may run scans, which includes asking nmap about one device. */
  canIdentify?: boolean
}

type State = { status: "loading" } | { status: "failed" } | ({ status: "ready" } & DeviceDetail)

export function DevicePanel({
  deviceId,
  onClose,
  canEdit = false,
  onChanged,
  canIdentify = false,
}: DevicePanelProps) {
  // The state remembers which device it belongs to, so a new deviceId shows the loading
  // state straight away without resetting state from inside the effect.
  const [loaded, setLoaded] = useState<{ id: number; state: State } | null>(null)
  const state: State = loaded?.id === deviceId ? loaded.state : { status: "loading" }
  const [editing, setEditing] = useState(false)
  const [reloads, setReloads] = useState(0)

  useEffect(() => {
    let cancelled = false
    getDeviceDetail(deviceId)
      .then((detail) => {
        if (!cancelled) setLoaded({ id: deviceId, state: { status: "ready", ...detail } })
      })
      .catch(() => {
        if (!cancelled) setLoaded({ id: deviceId, state: { status: "failed" } })
      })
    return () => {
      cancelled = true
    }
  }, [deviceId, reloads])

  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent aria-describedby={undefined}>
        <SheetTitle className="pr-8 text-lg font-semibold">
          {state.status === "ready" ? displayName(state.device) : "Device"}
        </SheetTitle>
        <SheetDescription className="sr-only">Details of the selected device</SheetDescription>
        {state.status === "ready" && editing ? (
          <DeviceForm
            // A new key when the data was reloaded, so the form starts from the current values.
            key={`${state.device.id}-${state.device.version}`}
            mode="edit"
            submitLabel="Save"
            initial={{
              label: state.device.label,
              notes: state.device.notes,
              kind: state.device.kind,
            }}
            onCancel={() => setEditing(false)}
            onReload={() => setReloads((n) => n + 1)}
            onSubmit={async (values) => {
              const device = await updateDevice(state.device.id, {
                label: values.label,
                notes: values.notes,
                kind: values.kind,
                version: state.device.version,
              })
              setLoaded({
                id: deviceId,
                state: {
                  status: "ready",
                  device,
                  interfaces: state.interfaces,
                  services: state.services,
                },
              })
              setEditing(false)
              onChanged?.()
            }}
          />
        ) : (
          <>
            <Body state={state} />
            {canIdentify && state.status === "ready" && state.device.ip ? (
              <IdentifyButton deviceId={deviceId} onDone={() => setReloads((n) => n + 1)} />
            ) : null}
            {canEdit && state.status === "ready" ? (
              <div className="flex gap-2">
                <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
                  <Pencil className="size-4" aria-hidden="true" />
                  Edit
                </Button>
                <DeleteDeviceButton
                  name={displayName(state.device)}
                  onConfirm={async () => {
                    await deleteDevice(state.device.id)
                    onChanged?.()
                    onClose()
                  }}
                />
              </div>
            ) : null}
          </>
        )}
      </SheetContent>
    </Sheet>
  )
}

/** How long the panel waits for nmap before it lets the person try again. */
const identifyWaitMs = 120_000

/**
 * Asks nmap about the device. The button shows only when the server found nmap. The answer
 * arrives as a topology change, then the panel loads the device again.
 */
function IdentifyButton({ deviceId, onDone }: { deviceId: number; onDone: () => void }) {
  const [available, setAvailable] = useState(false)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState("")

  useEffect(() => {
    let cancelled = false
    getNmap()
      .then((s) => !cancelled && setAvailable(s.available))
      .catch(() => !cancelled && setAvailable(false))
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (!running) return
    const timer = setTimeout(() => setRunning(false), identifyWaitMs)
    const close = openEvents({
      "topology.changed": () => {
        setRunning(false)
        onDone()
      },
    })
    return () => {
      clearTimeout(timer)
      close()
    }
  }, [running, onDone])

  if (!available) return null

  async function start() {
    setError("")
    try {
      await identifyDevice(deviceId)
      setRunning(true)
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        setError("A scan or another identification is running.")
      } else if (e instanceof ApiError && e.status === 400) {
        setError("Nmap is not available.")
      } else {
        setError("Could not start Nmap.")
      }
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <Button variant="outline" size="sm" disabled={running} onClick={() => void start()}>
        <ScanSearch className="size-4" aria-hidden="true" />
        {running ? "Asking Nmap…" : "Identify with Nmap"}
      </Button>
      {error ? <Alert variant="destructive">{error}</Alert> : null}
    </div>
  )
}

function Body({ state }: { state: State }) {
  if (state.status === "loading") {
    return (
      <div role="status" aria-label="Loading device" className="flex flex-col gap-2">
        <Skeleton className="h-6 w-2/3" />
        <Skeleton className="h-6 w-full" />
        <Skeleton className="h-6 w-full" />
      </div>
    )
  }
  if (state.status === "failed") {
    return <Alert variant="destructive">The device could not be loaded.</Alert>
  }
  return (
    <Tabs defaultValue="details">
      <TabsList aria-label="Device sections">
        <TabsTrigger value="details">Details</TabsTrigger>
        <TabsTrigger value="interfaces">Interfaces ({state.interfaces.length})</TabsTrigger>
        <TabsTrigger value="services">Services ({state.services.length})</TabsTrigger>
      </TabsList>
      <TabsContent value="details">
        <Details device={state.device} />
      </TabsContent>
      <TabsContent value="interfaces">
        <InterfacesTable interfaces={state.interfaces} />
      </TabsContent>
      <TabsContent value="services">
        <ServicesTable services={state.services} />
      </TabsContent>
    </Tabs>
  )
}

function Details({ device: d }: { device: Device }) {
  const rows: [string, React.ReactNode][] = [
    ["Status", <StatusBadge key="s" online={d.online} />],
    ["Kind", kindLabels[d.kind]],
    ["IP", d.ip || "–"],
    ["MAC", d.mac || "–"],
    ["Hostname", d.hostname || "–"],
    ["Vendor", d.vendor || "–"],
    ["Source", d.manual ? "Added by hand" : d.source || "–"],
    ["First seen", formatTime(d.firstSeenAt)],
    ["Last seen", formatTime(d.lastSeenAt)],
  ]
  return (
    <dl className="grid grid-cols-[7rem_1fr] gap-x-3 gap-y-2 text-sm">
      {rows.map(([name, value]) => (
        <div key={name} className="contents">
          <dt className="text-muted-foreground">{name}</dt>
          <dd className="font-mono break-all">{value}</dd>
        </div>
      ))}
      {d.notes ? (
        <div className="contents">
          <dt className="text-muted-foreground">Notes</dt>
          <dd className="break-words whitespace-pre-wrap">{d.notes}</dd>
        </div>
      ) : null}
      {d.description ? (
        <div className="contents">
          <dt className="text-muted-foreground">Description</dt>
          <dd className="break-words">{d.description}</dd>
        </div>
      ) : null}
      {d.osGuess ? (
        <div className="contents">
          <dt className="text-muted-foreground">System (Nmap)</dt>
          <dd className="break-words">{d.osGuess}</dd>
        </div>
      ) : null}
    </dl>
  )
}

function formatTime(iso: string): string {
  const t = new Date(iso)
  return Number.isNaN(t.getTime()) ? "–" : t.toLocaleString()
}

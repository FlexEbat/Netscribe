import { useEffect, useState } from "react"
import { Pencil } from "lucide-react"
import { deleteDevice, getDevice, updateDevice, type Device } from "@/api/client"
import { DeleteDeviceButton } from "@/components/DeleteDeviceButton"
import { DeviceForm } from "@/components/DeviceForm"
import { StatusBadge } from "@/components/StatusBadge"
import { Button } from "@/components/ui/button"
import { Alert } from "@/components/ui/alert"
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { displayName, kindLabels } from "@/lib/topology"

interface DevicePanelProps {
  deviceId: number
  onClose: () => void
  /** The user may edit and delete devices. */
  canEdit?: boolean
  /** Called after a device was changed or deleted, so the canvas can load the new state. */
  onChanged?: () => void
}

type State = { status: "loading" } | { status: "failed" } | { status: "ready"; device: Device }

export function DevicePanel({ deviceId, onClose, canEdit = false, onChanged }: DevicePanelProps) {
  // The state remembers which device it belongs to, so a new deviceId shows the loading
  // state straight away without resetting state from inside the effect.
  const [loaded, setLoaded] = useState<{ id: number; state: State } | null>(null)
  const state: State = loaded?.id === deviceId ? loaded.state : { status: "loading" }
  const [editing, setEditing] = useState(false)
  const [reloads, setReloads] = useState(0)

  useEffect(() => {
    let cancelled = false
    getDevice(deviceId)
      .then((device) => {
        if (!cancelled) setLoaded({ id: deviceId, state: { status: "ready", device } })
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
              setLoaded({ id: deviceId, state: { status: "ready", device } })
              setEditing(false)
              onChanged?.()
            }}
          />
        ) : (
          <>
            <Body state={state} />
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
  const d = state.device
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
    </dl>
  )
}

function formatTime(iso: string): string {
  const t = new Date(iso)
  return Number.isNaN(t.getTime()) ? "–" : t.toLocaleString()
}

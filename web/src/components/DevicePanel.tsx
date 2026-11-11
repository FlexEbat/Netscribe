import { PencilSimple, Power, Scan } from "@phosphor-icons/react"
import { useEffect, useState } from "react"
import {
  ApiError,
  deleteDevice,
  getDeviceDetail,
  getNmap,
  identifyDevice,
  listGroups,
  updateDevice,
  wakeDevice,
  type Device,
  type DeviceDetail,
  type Group,
} from "@/api/client"
import { openEvents } from "@/api/events"
import { DeleteDeviceButton } from "@/components/DeleteDeviceButton"
import { DeviceForm } from "@/components/DeviceForm"
import { HistoryTab } from "@/components/HistoryTab"
import { InterfacesTable } from "@/components/InterfacesTable"
import { ServicesTable } from "@/components/ServicesTable"
import { StatusBadge } from "@/components/StatusBadge"
import { UnknownBadge } from "@/components/TrustBadge"
import { Button } from "@/components/ui/button"
import { Alert } from "@/components/ui/alert"
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { displayName, kindLabels } from "@/lib/topology"
import { formatDateTime, t } from "@/i18n"

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
  const [groups, setGroups] = useState<Group[]>([])

  // Only an editor can change the group, so only an editor needs the list.
  useEffect(() => {
    if (!canEdit) return
    let cancelled = false
    listGroups()
      .then((g) => !cancelled && setGroups(g))
      .catch(() => !cancelled && setGroups([]))
    return () => {
      cancelled = true
    }
  }, [canEdit])

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
          {state.status === "ready" ? displayName(state.device) : t("devicePanel.device")}
        </SheetTitle>
        <SheetDescription className="sr-only">
          {t("devicePanel.detailsOfTheSelectedDevice")}
        </SheetDescription>
        {state.status === "ready" && editing ? (
          <DeviceForm
            // A new key when the data was reloaded, so the form starts from the current values.
            key={`${state.device.id}-${state.device.version}`}
            mode="edit"
            submitLabel={t("devicePanel.save")}
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
            {canIdentify && state.status === "ready" && state.device.ip && state.device.mac ? (
              <WakeButton deviceId={deviceId} />
            ) : null}
            {canIdentify && state.status === "ready" && state.device.ip ? (
              <IdentifyButton deviceId={deviceId} onDone={() => setReloads((n) => n + 1)} />
            ) : null}
            {canEdit && state.status === "ready" ? (
              <div className="flex gap-2">
                <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
                  <PencilSimple className="size-4" aria-hidden="true" />
                  {t("devicePanel.edit")}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    const d = state.device
                    updateDevice(d.id, { trusted: !d.trusted, version: d.version })
                      .then((device) => {
                        setLoaded({
                          id: deviceId,
                          state: {
                            status: "ready",
                            device,
                            interfaces: state.interfaces,
                            services: state.services,
                          },
                        })
                        onChanged?.()
                      })
                      // A conflict or a failure: load the current state.
                      .catch(() => setReloads((n) => n + 1))
                  }}
                >
                  {t(state.device.trusted ? "devicePanel.markUnknown" : "devicePanel.markTrusted")}
                </Button>
                <select
                  aria-label={t("devicePanel.groupOfTheDevice")}
                  className="h-8 rounded-sm border bg-input px-2 text-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                  value={state.device.groupId === null ? "" : String(state.device.groupId)}
                  onChange={(e) => {
                    const d = state.device
                    updateDevice(d.id, {
                      groupId: e.target.value === "" ? 0 : Number(e.target.value),
                      version: d.version,
                    })
                      .then(() => {
                        setReloads((n) => n + 1)
                        onChanged?.()
                      })
                      .catch(() => setReloads((n) => n + 1))
                  }}
                >
                  <option value="">
                    {t(
                      state.device.groupAuto ? "devicePanel.groupFromRange" : "devicePanel.noGroup",
                    )}
                  </option>
                  {groups.map((g) => (
                    <option key={g.id} value={String(g.id)}>
                      {g.name}
                    </option>
                  ))}
                </select>
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
        setError(t("devicePanel.scanRunning"))
      } else if (e instanceof ApiError && e.status === 400) {
        setError(t("devicePanel.nmapUnavailable"))
      } else {
        setError(t("devicePanel.nmapCouldNotStart"))
      }
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <Button variant="outline" size="sm" disabled={running} onClick={() => void start()}>
        <Scan className="size-4" aria-hidden="true" />
        {t(running ? "devicePanel.askingNmap" : "devicePanel.identifyWithNmap")}
      </Button>
      {error ? <Alert variant="destructive">{error}</Alert> : null}
    </div>
  )
}

/**
 * Sends a Wake on LAN packet. The server answers 202 as soon as the packet is out, and the
 * panel can only say that it was sent: it cannot know whether the machine started.
 */
function WakeButton({ deviceId }: { deviceId: number }) {
  const [sent, setSent] = useState(false)
  const [error, setError] = useState("")

  async function wake() {
    setError("")
    setSent(false)
    try {
      await wakeDevice(deviceId)
      setSent(true)
    } catch (e) {
      if (e instanceof ApiError && (e.status === 422 || e.status === 429)) {
        setError(e.message)
      } else {
        setError(t("devicePanel.packetFailed"))
      }
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <Button variant="outline" size="sm" onClick={() => void wake()}>
        <Power className="size-4" aria-hidden="true" />
        {t("devicePanel.wakeOnLan")}
      </Button>
      {sent ? (
        <p role="status" className="text-sm text-muted-foreground">
          {t("devicePanel.thePacketWasSentThe")}
        </p>
      ) : null}
      {error ? <Alert variant="destructive">{error}</Alert> : null}
    </div>
  )
}

function Body({ state }: { state: State }) {
  if (state.status === "loading") {
    return (
      <div
        role="status"
        aria-label={t("devicePanel.loadingDevice")}
        className="flex flex-col gap-2"
      >
        <Skeleton className="h-6 w-2/3" />
        <Skeleton className="h-6 w-full" />
        <Skeleton className="h-6 w-full" />
      </div>
    )
  }
  if (state.status === "failed") {
    return <Alert variant="destructive">{t("devicePanel.theDeviceCouldNotBe")}</Alert>
  }
  return (
    <Tabs defaultValue="details">
      <TabsList aria-label={t("devicePanel.deviceSections")}>
        <TabsTrigger value="details">{t("devicePanel.details")}</TabsTrigger>
        <TabsTrigger value="interfaces">
          {t("devicePanel.interfacesCount", { n: state.interfaces.length })}
        </TabsTrigger>
        <TabsTrigger value="services">
          {t("devicePanel.servicesCount", { n: state.services.length })}
        </TabsTrigger>
        <TabsTrigger value="history">{t("devicePanel.history")}</TabsTrigger>
      </TabsList>
      <TabsContent value="details">
        <Details device={state.device} />
      </TabsContent>
      <TabsContent value="interfaces">
        <InterfacesTable interfaces={state.interfaces} />
      </TabsContent>
      <TabsContent value="services">
        <ServicesTable services={state.services} address={state.device.ip || state.device.ip6} />
      </TabsContent>
      <TabsContent value="history">
        <HistoryTab deviceId={state.device.id} />
      </TabsContent>
    </Tabs>
  )
}

/** The kind of a device with how sure the classifier was and why, or who set it. */
function KindCell({ device: d }: { device: Device }) {
  const hint = d.kindLocked
    ? t("devicePanel.setByPerson")
    : d.kindScore > 0
      ? t("devicePanel.kindScore", { n: d.kindScore })
      : ""
  const reasons = d.kindLocked ? [] : d.kindReasons
  return (
    <div className="flex flex-col gap-1">
      <span>
        {kindLabels[d.kind]}
        {hint ? <span className="text-muted-foreground"> ({hint})</span> : null}
      </span>
      {reasons.length > 0 ? (
        <ul className="list-disc pl-4 text-xs text-muted-foreground">
          {reasons.map((r) => (
            <li key={r}>{r}</li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}

function Details({ device: d }: { device: Device }) {
  const group = d.group
    ? `${d.group.name}${d.group.vlan ? t("devicePanel.vlan", { n: d.group.vlan }) : ""}${d.groupAuto ? t("devicePanel.fromRange") : ""}`
    : t("devicePanel.none")
  const rows: [string, React.ReactNode][] = [
    [t("devicePanel.status"), <StatusBadge key="s" online={d.online} />],
    [t("devicePanel.trust"), d.trusted ? t("devicePanel.trusted") : <UnknownBadge key="t" />],
    [t("devicePanel.group"), group],
    [t("devicePanel.kind"), <KindCell key="k" device={d} />],
    ["IP", d.ip || "–"],
    [t("devicePanel.ipv6"), d.ip6 || "–"],
    ["MAC", d.mac || "–"],
    [t("devicePanel.hostname"), d.hostname || "–"],
    [t("devicePanel.vendor"), d.vendor || "–"],
    [t("devicePanel.source"), d.manual ? t("devicePanel.addedByHand") : d.source || "–"],
    [t("devicePanel.firstSeen"), formatDateTime(d.firstSeenAt)],
    [t("devicePanel.lastSeen"), formatDateTime(d.lastSeenAt)],
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
          <dt className="text-muted-foreground">{t("devicePanel.notes")}</dt>
          <dd className="break-words whitespace-pre-wrap">{d.notes}</dd>
        </div>
      ) : null}
      {d.description ? (
        <div className="contents">
          <dt className="text-muted-foreground">{t("devicePanel.description")}</dt>
          <dd className="break-words">{d.description}</dd>
        </div>
      ) : null}
      {d.osGuess ? (
        <div className="contents">
          <dt className="text-muted-foreground">{t("devicePanel.systemNmap")}</dt>
          <dd className="break-words">{d.osGuess}</dd>
        </div>
      ) : null}
    </dl>
  )
}

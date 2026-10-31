import { useCallback, useEffect, useRef, useState } from "react"
import { useSearchParams } from "react-router-dom"
import {
  ApiError,
  getNmap,
  listDevices,
  bulkEdit,
  listGroups,
  listScans,
  startScan,
  updateDevice,
  type BulkChange,
  type Group,
  type Device,
  type NmapState,
  type Scan,
} from "@/api/client"
import { openEvents, type ScanProgress } from "@/api/events"
import { useSession } from "@/api/session"
import { AddDeviceDialog } from "@/components/AddDeviceDialog"
import { BulkBar } from "@/components/BulkBar"
import { EmptyState } from "@/components/EmptyState"
import { ImportDialog } from "@/components/ImportDialog"
import { SavedFilters } from "@/components/SavedFilters"
import { ScanBar } from "@/components/ScanBar"
import { Button } from "@/components/ui/button"
import { StatusBadge } from "@/components/StatusBadge"
import { UnknownBadge } from "@/components/TrustBadge"
import { Alert } from "@/components/ui/alert"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { sortByIp } from "@/lib/ip"
import { can } from "@/lib/permissions"

type Filter = "all" | "online" | "offline"
type TrustFilter = "all" | "unknown" | "trusted"

type State = { status: "loading" } | { status: "failed" } | { status: "ready"; devices: Device[] }

export function Inventory() {
  const { state: session } = useSession()
  const canRun = session.status === "signedIn" && can(session.session.permissions, "scans:run")
  const canWrite =
    session.status === "signedIn" && can(session.session.permissions, "devices:write")

  const searchRef = useRef<HTMLInputElement>(null)
  const [nmap, setNmap] = useState<NmapState | null>(null)
  const [useNmap, setUseNmap] = useState(false)
  const [search, setSearch] = useState("")
  // The overview links here with ?status=offline or ?trust=unknown.
  const [params] = useSearchParams()
  const [filter, setFilter] = useState<Filter>(() => {
    const v = params.get("status")
    return v === "online" || v === "offline" ? v : "all"
  })
  const [trust, setTrust] = useState<TrustFilter>(() => {
    const v = params.get("trust")
    return v === "unknown" || v === "trusted" ? v : "all"
  })
  const [groupFilter, setGroupFilter] = useState(() => {
    const v = params.get("group")
    return v === "none" || (v !== null && /^[1-9][0-9]*$/.test(v)) ? v : "all"
  })
  const [groups, setGroups] = useState<Group[]>([])
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [state, setState] = useState<State>({ status: "loading" })
  const [refresh, setRefresh] = useState(0)
  const [scan, setScan] = useState<Scan | null>(null)
  const [progress, setProgress] = useState<ScanProgress | null>(null)
  const [starting, setStarting] = useState(false)
  const [scanError, setScanError] = useState("")

  // Whether nmap can be used decides if the scan bar offers it. A failure hides the option.
  useEffect(() => {
    if (!canRun) return
    let cancelled = false
    getNmap()
      .then((state) => !cancelled && setNmap(state))
      .catch(() => !cancelled && setNmap(null))
    return () => {
      cancelled = true
    }
  }, [canRun])

  // The groups fill the filter and the bulk bar. A failure leaves the lists without them.
  useEffect(() => {
    let cancelled = false
    listGroups()
      .then((g) => !cancelled && setGroups(g))
      .catch(() => !cancelled && setGroups([]))
    return () => {
      cancelled = true
    }
  }, [refresh])

  // "/" jumps to the search field, unless the user is already typing somewhere.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "/" || e.ctrlKey || e.metaKey || e.altKey) return
      const target = e.target
      if (
        target instanceof HTMLElement &&
        (target.isContentEditable || ["INPUT", "SELECT", "TEXTAREA"].includes(target.tagName))
      ) {
        return
      }
      e.preventDefault()
      searchRef.current?.focus()
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [])

  useEffect(() => {
    let cancelled = false
    const online = filter === "all" ? undefined : filter === "online"
    const trusted = trust === "all" ? undefined : trust === "trusted"
    const group =
      groupFilter === "all" ? undefined : groupFilter === "none" ? "none" : Number(groupFilter)
    listDevices({ q: search.trim(), online, trusted, group })
      .then((devices) => {
        if (!cancelled) setState({ status: "ready", devices: sortByIp(devices) })
      })
      .catch(() => {
        if (!cancelled) setState({ status: "failed" })
      })
    return () => {
      cancelled = true
    }
  }, [search, filter, trust, groupFilter, refresh])

  useEffect(() => {
    let cancelled = false
    listScans()
      .then((scans) => {
        if (!cancelled) setScan(scans[0] ?? null)
      })
      .catch(() => {
        // The bar then says no scan has run; the device list reports its own failure.
      })
    const close = openEvents({
      "scan.started": (s) => {
        setScan(s)
        setProgress(null)
        setStarting(false)
      },
      "scan.progress": setProgress,
      "scan.finished": (s) => {
        setScan(s)
        setProgress(null)
      },
      "topology.changed": () => setRefresh((n) => n + 1),
    })
    return () => {
      cancelled = true
      close()
    }
  }, [])

  const run = useCallback(async () => {
    setScanError("")
    setStarting(true)
    try {
      setScan(await startScan(useNmap))
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        setScanError("A scan is already running.")
      } else if (error instanceof ApiError && error.status === 400) {
        setScanError(
          useNmap ? "Nmap is not available on the server." : "No scan targets are configured.",
        )
      } else {
        setScanError("The scan could not be started.")
      }
    } finally {
      setStarting(false)
    }
  }, [useNmap])

  const currentFilter = (() => {
    const p = new URLSearchParams()
    if (search.trim() !== "") p.set("q", search.trim())
    if (filter !== "all") p.set("status", filter)
    if (trust !== "all") p.set("trust", trust)
    if (groupFilter !== "all") p.set("group", groupFilter)
    return p.toString()
  })()
  const filtering =
    search.trim() !== "" || filter !== "all" || trust !== "all" || groupFilter !== "all"

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold">Inventory</h1>
        {canWrite ? (
          <div className="flex gap-2">
            <ImportDialog onImported={() => setRefresh((n) => n + 1)} />
            <AddDeviceDialog onCreated={() => setRefresh((n) => n + 1)} />
          </div>
        ) : null}
      </div>
      <ScanBar
        scan={scan}
        progress={progress}
        canRun={canRun}
        starting={starting}
        error={scanError}
        onStart={() => void run()}
        nmap={nmap}
        useNmap={useNmap}
        onUseNmapChange={setUseNmap}
      />
      <div className="flex flex-wrap gap-3">
        <Input
          ref={searchRef}
          type="search"
          aria-label="Search devices"
          aria-keyshortcuts="/"
          placeholder="Search by IP, MAC, hostname or vendor"
          className="max-w-sm"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <select
          aria-label="Filter by status"
          className="h-9 rounded-lg border bg-input px-3 text-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
          value={filter}
          onChange={(e) => setFilter(e.target.value as Filter)}
        >
          <option value="all">All devices</option>
          <option value="online">Online</option>
          <option value="offline">Offline</option>
        </select>
        <select
          aria-label="Filter by trust"
          className="h-9 rounded-lg border bg-input px-3 text-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
          value={trust}
          onChange={(e) => setTrust(e.target.value as TrustFilter)}
        >
          <option value="all">Known and unknown</option>
          <option value="unknown">Unknown only</option>
          <option value="trusted">Trusted only</option>
        </select>
        <select
          aria-label="Filter by group"
          className="h-9 rounded-lg border bg-input px-3 text-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
          value={groupFilter}
          onChange={(e) => setGroupFilter(e.target.value)}
        >
          <option value="all">All groups</option>
          <option value="none">No group</option>
          {groups.map((g) => (
            <option key={g.id} value={String(g.id)}>
              {g.name}
            </option>
          ))}
        </select>
        <SavedFilters
          page="inventory"
          current={currentFilter}
          onApply={(query) => {
            // A saved filter sets every field of the page: what it does not name is cleared.
            const p = new URLSearchParams(query)
            setSearch(p.get("q") ?? "")
            const status = p.get("status")
            setFilter(status === "online" || status === "offline" ? status : "all")
            const trusted = p.get("trust")
            setTrust(trusted === "unknown" || trusted === "trusted" ? trusted : "all")
            setGroupFilter(p.get("group") ?? "all")
          }}
        />
      </div>
      {canWrite && selected.size > 0 ? (
        <BulkBar
          count={selected.size}
          groups={groups}
          onClear={() => setSelected(new Set())}
          onApply={async (change: BulkChange) => {
            await bulkEdit([...selected], change)
            setSelected(new Set())
            setRefresh((n) => n + 1)
          }}
        />
      ) : null}
      <Results
        state={state}
        filtering={filtering}
        canWrite={canWrite}
        selected={selected}
        onSelect={setSelected}
        onTrusted={() => setRefresh((n) => n + 1)}
        firstScan={canRun && scan === null ? () => void run() : undefined}
      />
    </div>
  )
}

function Results({
  state,
  filtering,
  canWrite,
  selected,
  onSelect,
  onTrusted,
  firstScan,
}: {
  state: State
  filtering: boolean
  canWrite: boolean
  selected: Set<number>
  onSelect: (next: Set<number>) => void
  onTrusted: () => void
  firstScan?: () => void
}) {
  if (state.status === "loading") {
    return (
      <div role="status" aria-label="Loading devices" className="flex flex-col gap-2">
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
      </div>
    )
  }
  if (state.status === "failed") {
    return <Alert variant="destructive">The devices could not be loaded. Try again later.</Alert>
  }
  if (state.devices.length === 0) {
    return filtering ? (
      <EmptyState title="No matches" text="No device matches the search and filter." />
    ) : (
      <EmptyState
        title="No devices yet"
        text="Run a scan to discover the devices on your network."
        action={firstScan ? <Button onClick={firstScan}>Run first scan</Button> : undefined}
      />
    )
  }
  return (
    <Table>
      <TableCaption>Devices ({state.devices.length})</TableCaption>
      <TableHeader>
        <TableRow>
          {canWrite ? (
            <TableHead className="w-8">
              <input
                type="checkbox"
                aria-label="Select all devices"
                checked={state.devices.length > 0 && state.devices.every((d) => selected.has(d.id))}
                onChange={(e) =>
                  onSelect(e.target.checked ? new Set(state.devices.map((d) => d.id)) : new Set())
                }
              />
            </TableHead>
          ) : null}
          <TableHead>IP</TableHead>
          <TableHead>MAC</TableHead>
          <TableHead>Hostname</TableHead>
          <TableHead>Kind</TableHead>
          <TableHead>Group</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Trust</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {state.devices.map((d) => (
          <TableRow key={d.id}>
            {canWrite ? (
              <TableCell>
                <input
                  type="checkbox"
                  aria-label={`Select ${d.label || d.hostname || d.ip || d.mac}`}
                  checked={selected.has(d.id)}
                  onChange={(e) => {
                    const next = new Set(selected)
                    if (e.target.checked) next.add(d.id)
                    else next.delete(d.id)
                    onSelect(next)
                  }}
                />
              </TableCell>
            ) : null}
            <TableCell className="font-mono">{d.ip || "–"}</TableCell>
            <TableCell className="font-mono">{d.mac || "–"}</TableCell>
            <TableCell>{d.label || d.hostname || "–"}</TableCell>
            <TableCell className="capitalize">{d.kind}</TableCell>
            <TableCell>
              {d.group ? (
                <span>
                  {d.group.name}
                  {d.groupAuto ? (
                    <span className="text-xs text-muted-foreground"> (range)</span>
                  ) : null}
                </span>
              ) : (
                "–"
              )}
            </TableCell>
            <TableCell>
              <StatusBadge online={d.online} />
            </TableCell>
            <TableCell>
              {d.trusted ? (
                <span className="text-muted-foreground">Trusted</span>
              ) : (
                <span className="flex flex-wrap items-center gap-2">
                  <UnknownBadge />
                  {canWrite ? <TrustButton device={d} onDone={onTrusted} /> : null}
                </span>
              )}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

/** Marks a device as known. A version that is out of date only reloads the list. */
function TrustButton({ device, onDone }: { device: Device; onDone: () => void }) {
  const [busy, setBusy] = useState(false)
  async function trust() {
    setBusy(true)
    try {
      await updateDevice(device.id, { trusted: true, version: device.version })
    } catch {
      // A conflict means someone changed the device; the reload shows the current state.
    } finally {
      setBusy(false)
      onDone()
    }
  }
  return (
    <Button
      variant="outline"
      size="sm"
      disabled={busy}
      aria-label={`Trust ${device.label || device.hostname || device.ip || device.mac}`}
      onClick={() => void trust()}
    >
      Trust
    </Button>
  )
}

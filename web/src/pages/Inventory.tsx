import { useCallback, useEffect, useState } from "react"
import { ApiError, listDevices, listScans, startScan, type Device, type Scan } from "@/api/client"
import { openEvents, type ScanProgress } from "@/api/events"
import { useSession } from "@/api/session"
import { EmptyState } from "@/components/EmptyState"
import { ScanBar } from "@/components/ScanBar"
import { Button } from "@/components/ui/button"
import { StatusBadge } from "@/components/StatusBadge"
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

type State = { status: "loading" } | { status: "failed" } | { status: "ready"; devices: Device[] }

export function Inventory() {
  const { state: session } = useSession()
  const canRun = session.status === "signedIn" && can(session.session.permissions, "scans:run")

  const [search, setSearch] = useState("")
  const [filter, setFilter] = useState<Filter>("all")
  const [state, setState] = useState<State>({ status: "loading" })
  const [refresh, setRefresh] = useState(0)
  const [scan, setScan] = useState<Scan | null>(null)
  const [progress, setProgress] = useState<ScanProgress | null>(null)
  const [starting, setStarting] = useState(false)
  const [scanError, setScanError] = useState("")

  useEffect(() => {
    let cancelled = false
    const online = filter === "all" ? undefined : filter === "online"
    listDevices({ q: search.trim(), online })
      .then((devices) => {
        if (!cancelled) setState({ status: "ready", devices: sortByIp(devices) })
      })
      .catch(() => {
        if (!cancelled) setState({ status: "failed" })
      })
    return () => {
      cancelled = true
    }
  }, [search, filter, refresh])

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
      setScan(await startScan())
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        setScanError("A scan is already running.")
      } else if (error instanceof ApiError && error.status === 400) {
        setScanError("No scan targets are configured.")
      } else {
        setScanError("The scan could not be started.")
      }
    } finally {
      setStarting(false)
    }
  }, [])

  const filtering = search.trim() !== "" || filter !== "all"

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-lg font-semibold">Inventory</h1>
      <ScanBar
        scan={scan}
        progress={progress}
        canRun={canRun}
        starting={starting}
        error={scanError}
        onStart={() => void run()}
      />
      <div className="flex flex-wrap gap-3">
        <Input
          type="search"
          aria-label="Search devices"
          placeholder="Search by IP, MAC, hostname or vendor"
          className="max-w-sm"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <select
          aria-label="Filter by status"
          className="h-9 rounded-lg border bg-background px-3 text-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
          value={filter}
          onChange={(e) => setFilter(e.target.value as Filter)}
        >
          <option value="all">All devices</option>
          <option value="online">Online</option>
          <option value="offline">Offline</option>
        </select>
      </div>
      <Results
        state={state}
        filtering={filtering}
        firstScan={canRun && scan === null ? () => void run() : undefined}
      />
    </div>
  )
}

function Results({
  state,
  filtering,
  firstScan,
}: {
  state: State
  filtering: boolean
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
          <TableHead>IP</TableHead>
          <TableHead>MAC</TableHead>
          <TableHead>Hostname</TableHead>
          <TableHead>Kind</TableHead>
          <TableHead>Status</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {state.devices.map((d) => (
          <TableRow key={d.id}>
            <TableCell className="font-mono">{d.ip || "–"}</TableCell>
            <TableCell className="font-mono">{d.mac || "–"}</TableCell>
            <TableCell>{d.label || d.hostname || "–"}</TableCell>
            <TableCell className="capitalize">{d.kind}</TableCell>
            <TableCell>
              <StatusBadge online={d.online} />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

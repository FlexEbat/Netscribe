import { useEffect, useState } from "react"
import { listDevices, type Device } from "@/api/client"
import { EmptyState } from "@/components/EmptyState"
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

type Filter = "all" | "online" | "offline"

type State = { status: "loading" } | { status: "failed" } | { status: "ready"; devices: Device[] }

export function Inventory() {
  const [search, setSearch] = useState("")
  const [filter, setFilter] = useState<Filter>("all")
  const [state, setState] = useState<State>({ status: "loading" })

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
  }, [search, filter])

  const filtering = search.trim() !== "" || filter !== "all"

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-lg font-semibold">Inventory</h1>
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
      <Results state={state} filtering={filtering} />
    </div>
  )
}

function Results({ state, filtering }: { state: State; filtering: boolean }) {
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

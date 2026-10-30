import { useEffect, useState } from "react"
import { listDevices, listServices, type Device, type Service } from "@/api/client"
import { EmptyState } from "@/components/EmptyState"
import { RiskBadge } from "@/components/RiskBadge"
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
import { compareIp } from "@/lib/ip"
import { filterServices } from "@/lib/services"

type State =
  | { status: "loading" }
  | { status: "failed" }
  | { status: "ready"; services: Service[]; devices: Map<number, Device> }

export function Services() {
  const [query, setQuery] = useState("")
  const [state, setState] = useState<State>({ status: "loading" })

  useEffect(() => {
    let cancelled = false
    Promise.all([listServices(), listDevices()])
      .then(([services, devices]) => {
        if (!cancelled) {
          setState({ status: "ready", services, devices: new Map(devices.map((d) => [d.id, d])) })
        }
      })
      .catch(() => {
        if (!cancelled) setState({ status: "failed" })
      })
    return () => {
      cancelled = true
    }
  }, [])

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-semibold">Services</h1>
      <Input
        type="search"
        aria-label="Filter by service name or banner"
        placeholder="Filter by service name or banner"
        className="max-w-sm"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      <Results state={state} query={query} />
    </div>
  )
}

function deviceName(d: Device | undefined): string {
  if (!d) return "–"
  return d.label || d.hostname || d.ip || d.mac || "–"
}

function Results({ state, query }: { state: State; query: string }) {
  if (state.status === "loading") {
    return (
      <div role="status" aria-label="Loading services" className="flex flex-col gap-2">
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
      </div>
    )
  }
  if (state.status === "failed") {
    return <Alert variant="destructive">The services could not be loaded. Try again later.</Alert>
  }
  if (state.services.length === 0) {
    return (
      <EmptyState
        title="No services yet"
        text="Open ports appear here after a scan. The ports to scan are set in scan.ports."
      />
    )
  }
  const shown = filterServices(state.services, query).sort((a, b) => {
    const byIp = compareIp(
      state.devices.get(a.deviceId)?.ip ?? "",
      state.devices.get(b.deviceId)?.ip ?? "",
    )
    return byIp || a.deviceId - b.deviceId || a.port - b.port
  })
  if (shown.length === 0) {
    return <EmptyState title="No matches" text="No service matches the filter." />
  }
  return (
    <Table>
      <TableCaption>Services ({shown.length})</TableCaption>
      <TableHeader>
        <TableRow>
          <TableHead>Service</TableHead>
          <TableHead>Port</TableHead>
          <TableHead>Protocol</TableHead>
          <TableHead>Banner</TableHead>
          <TableHead>Device</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {shown.map((s) => {
          const d = state.devices.get(s.deviceId)
          return (
            <TableRow key={s.id}>
              <TableCell className="flex flex-wrap items-center gap-2">
                {s.name || "–"}
                {s.risk ? <RiskBadge risk={s.risk} /> : null}
              </TableCell>
              <TableCell className="font-mono">{s.port}</TableCell>
              <TableCell className="uppercase">{s.proto}</TableCell>
              <TableCell className="max-w-64 break-words text-muted-foreground">
                {s.banner || "–"}
              </TableCell>
              <TableCell>
                {deviceName(d)}
                {d?.ip && deviceName(d) !== d.ip ? (
                  <span className="ml-2 font-mono text-muted-foreground">{d.ip}</span>
                ) : null}
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}

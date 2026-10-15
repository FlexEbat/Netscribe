import { useCallback, useEffect, useState } from "react"
import {
  listContainers,
  listDevices,
  type Container,
  type ContainerList,
  type Device,
} from "@/api/client"
import { openEvents } from "@/api/events"
import { EmptyState } from "@/components/EmptyState"
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
import { filterContainers, formatPort, stateClasses, uniquePorts } from "@/lib/containers"
import { compareIp } from "@/lib/ip"
import { displayName } from "@/lib/topology"

type State =
  | { status: "loading" }
  | { status: "failed" }
  | { status: "ready"; data: ContainerList; devices: Map<number, Device> }

export function Containers() {
  const [query, setQuery] = useState("")
  const [state, setState] = useState<State>({ status: "loading" })

  const load = useCallback(() => {
    Promise.all([listContainers(), listDevices()])
      .then(([data, devices]) =>
        setState({ status: "ready", data, devices: new Map(devices.map((d) => [d.id, d])) }),
      )
      .catch(() => setState((s) => (s.status === "ready" ? s : { status: "failed" })))
  }, [])

  // The server announces every change of the containers, a start or a stop shows up at once.
  useEffect(() => {
    load()
    return openEvents({ "topology.changed": load })
  }, [load])

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-lg font-semibold">Containers</h1>
      <Input
        type="search"
        aria-label="Filter containers"
        placeholder="Filter by name, image, state or project"
        className="max-w-sm"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      <Results state={state} query={query} />
    </div>
  )
}

function hostName(d: Device | undefined): string {
  return d ? displayName(d) : "–"
}

function Results({ state, query }: { state: State; query: string }) {
  if (state.status === "loading") {
    return (
      <div role="status" aria-label="Loading containers" className="flex flex-col gap-2">
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
      </div>
    )
  }
  if (state.status === "failed") {
    return <Alert variant="destructive">The containers could not be loaded. Try again later.</Alert>
  }
  const { data, devices } = state
  if (!data.docker.enabled) {
    return (
      <EmptyState
        title="Docker is turned off"
        text="Containers are not collected. Set docker.enabled to true in the config to turn it on."
      />
    )
  }
  if (!data.docker.available) {
    return (
      <EmptyState
        title="Docker cannot be read"
        text={
          data.docker.reason
            ? `${capitalize(data.docker.reason)}. Containers appear here as soon as the daemon answers.`
            : "Containers appear here as soon as the daemon answers."
        }
      />
    )
  }
  if (data.containers.length === 0) {
    return (
      <EmptyState
        title="No containers yet"
        text="This host runs no containers, or the first scan has not finished."
      />
    )
  }
  const shown = sorted(filterContainers(data.containers, query), devices)
  if (shown.length === 0) {
    return <EmptyState title="No matches" text="No container matches the filter." />
  }
  return (
    <Table>
      <TableCaption>Containers ({shown.length})</TableCaption>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead>Image</TableHead>
          <TableHead>State</TableHead>
          <TableHead>Ports</TableHead>
          <TableHead>Networks</TableHead>
          <TableHead>Project</TableHead>
          <TableHead>Host</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {shown.map((c) => (
          <TableRow key={c.id}>
            <TableCell className="font-medium">{c.name}</TableCell>
            <TableCell className="font-mono text-xs">{c.image}</TableCell>
            <TableCell>
              <span
                className={`rounded border px-1.5 py-0.5 text-xs font-medium ${stateClasses(c.state)}`}
              >
                {c.state}
              </span>
            </TableCell>
            <TableCell className="font-mono text-xs">
              {uniquePorts(c.ports).map((p) => (
                <div key={`${p.hostIp}-${p.hostPort}-${p.containerPort}-${p.proto}`}>
                  {formatPort(p)}
                </div>
              ))}
            </TableCell>
            <TableCell className="text-xs">
              {c.networks.map((n) => (
                <div key={n.name}>
                  {n.name}
                  {n.ip ? (
                    <span className="ml-1 font-mono text-muted-foreground">{n.ip}</span>
                  ) : null}
                </div>
              ))}
            </TableCell>
            <TableCell>{c.composeProject || "–"}</TableCell>
            <TableCell>{hostName(devices.get(c.deviceId))}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1)
}

function sorted(list: Container[], devices: Map<number, Device>): Container[] {
  return list.sort((a, b) => {
    const byIp = compareIp(devices.get(a.deviceId)?.ip ?? "", devices.get(b.deviceId)?.ip ?? "")
    return byIp || a.deviceId - b.deviceId || a.name.localeCompare(b.name)
  })
}

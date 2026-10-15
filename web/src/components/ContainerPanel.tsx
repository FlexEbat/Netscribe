import type { Container } from "@/api/client"
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet"
import { formatPort, stateClasses, uniquePorts } from "@/lib/containers"

interface ContainerPanelProps {
  container: Container
  /** The name of the host device the container runs on. */
  hostName: string
  onClose: () => void
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-sm">{children}</dd>
    </div>
  )
}

function when(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? "–" : d.toLocaleString()
}

/** Everything the daemon told about one container. It only reads: nothing here changes a container. */
export function ContainerPanel({ container, hostName, onClose }: ContainerPanelProps) {
  const ports = uniquePorts(container.ports)
  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent aria-describedby={undefined}>
        <SheetTitle className="pr-8 text-lg font-semibold">{container.name}</SheetTitle>
        <SheetDescription className="sr-only">Details of the selected container</SheetDescription>
        <dl className="flex flex-col gap-3">
          <Row label="State">
            <span
              className={`rounded border px-1.5 py-0.5 text-xs font-medium ${stateClasses(container.state)}`}
            >
              {container.state}
            </span>
          </Row>
          <Row label="Image">
            <span className="break-all font-mono">{container.image}</span>
          </Row>
          <Row label="Host">{hostName}</Row>
          <Row label="Compose project">{container.composeProject || "–"}</Row>
          <Row label="Ports">
            {ports.length === 0 ? (
              "–"
            ) : (
              <ul className="font-mono">
                {ports.map((p) => (
                  <li key={`${p.hostIp}-${p.hostPort}-${p.containerPort}-${p.proto}`}>
                    {formatPort(p)}
                  </li>
                ))}
              </ul>
            )}
          </Row>
          <Row label="Networks">
            {container.networks.length === 0 ? (
              "–"
            ) : (
              <ul>
                {container.networks.map((n) => (
                  <li key={n.name}>
                    {n.name}
                    {n.ip ? (
                      <span className="ml-2 font-mono text-muted-foreground">{n.ip}</span>
                    ) : null}
                  </li>
                ))}
              </ul>
            )}
          </Row>
          <Row label="First seen">{when(container.firstSeenAt)}</Row>
          <Row label="Last seen">{when(container.lastSeenAt)}</Row>
        </dl>
      </SheetContent>
    </Sheet>
  )
}

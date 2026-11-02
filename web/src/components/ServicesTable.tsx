import type { Service } from "@/api/client"
import { RiskBadge } from "@/components/RiskBadge"
import { ServiceLink } from "@/components/ServiceLink"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

/** The open ports of one device. */
export function ServicesTable({
  services,
  address,
}: {
  services: readonly Service[]
  /** The stored address of the device, from which the links are built. */
  address: string
}) {
  if (services.length === 0) {
    return <p className="text-sm text-muted-foreground">No open ports were found on this device.</p>
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Service</TableHead>
          <TableHead>Port</TableHead>
          <TableHead>Protocol</TableHead>
          <TableHead>Banner</TableHead>
          <TableHead>Open</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {services.map((s) => (
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
              <ServiceLink address={address} service={s} />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

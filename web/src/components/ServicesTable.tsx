import type { Service } from "@/api/client"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

/** The open ports of one device. */
export function ServicesTable({ services }: { services: readonly Service[] }) {
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
        </TableRow>
      </TableHeader>
      <TableBody>
        {services.map((s) => (
          <TableRow key={s.id}>
            <TableCell>{s.name || "–"}</TableCell>
            <TableCell className="font-mono">{s.port}</TableCell>
            <TableCell className="uppercase">{s.proto}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

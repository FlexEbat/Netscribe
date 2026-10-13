import type { DeviceInterface } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

/** The interfaces of one device: name, MAC, IP and whether the link is up. */
export function InterfacesTable({ interfaces }: { interfaces: readonly DeviceInterface[] }) {
  if (interfaces.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">No interfaces were read from this device.</p>
    )
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead>MAC</TableHead>
          <TableHead>IP</TableHead>
          <TableHead>State</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {interfaces.map((i) => (
          <TableRow key={i.id}>
            <TableCell className="font-mono break-all">{i.name}</TableCell>
            <TableCell className="font-mono break-all">{i.mac || "–"}</TableCell>
            <TableCell className="font-mono break-all">{i.ip || "–"}</TableCell>
            <TableCell>
              <Badge variant={i.up ? "default" : "outline"}>{i.up ? "Up" : "Down"}</Badge>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

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
import { t } from "@/i18n"

/** The interfaces of one device: name, MAC, IP and whether the link is up. */
export function InterfacesTable({ interfaces }: { interfaces: readonly DeviceInterface[] }) {
  if (interfaces.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        {t("interfacesTable.noInterfacesWereReadFrom")}
      </p>
    )
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t("interfacesTable.name")}</TableHead>
          <TableHead>{t("interfacesTable.mac")}</TableHead>
          <TableHead>{t("interfacesTable.ip")}</TableHead>
          <TableHead>{t("interfacesTable.state")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {interfaces.map((i) => (
          <TableRow key={i.id}>
            <TableCell className="font-mono break-all">{i.name}</TableCell>
            <TableCell className="font-mono break-all">{i.mac || "–"}</TableCell>
            <TableCell className="font-mono break-all">{i.ip || "–"}</TableCell>
            <TableCell>
              <Badge variant={i.up ? "default" : "outline"}>
                {t(i.up ? "interfacesTable.up" : "interfacesTable.down")}
              </Badge>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

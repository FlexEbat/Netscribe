import { Badge } from "@/components/ui/badge"

/** Marks a device that nobody has confirmed yet. A trusted device shows nothing. */
export function UnknownBadge() {
  return (
    <Badge variant="outline" title="Nobody has marked this device as known">
      Unknown
    </Badge>
  )
}

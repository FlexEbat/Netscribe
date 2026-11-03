import { Badge } from "@/components/ui/badge"
import { t } from "@/i18n"

/** Marks a device that nobody has confirmed yet. A trusted device shows nothing. */
export function UnknownBadge() {
  return (
    <Badge variant="outline" title={t("trustBadge.nobodyHasMarkedThisDevice")}>
      {t("trustBadge.unknown")}
    </Badge>
  )
}

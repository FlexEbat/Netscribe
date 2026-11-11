import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import { t } from "@/i18n"

export function StatusBadge({ online }: { online: boolean }) {
  return (
    <Badge
      variant={online ? "default" : "outline"}
      className={cn("gap-2 bg-transparent", online && "border-ok/50 text-ok")}
    >
      <span aria-hidden="true" className="relative inline-flex size-2">
        {online ? (
          <span className="absolute inset-0 animate-[beacon_2.4s_cubic-bezier(0.22,1,0.36,1)_infinite] rounded-sm bg-ok" />
        ) : null}
        <span
          className={cn("relative size-2 rounded-sm", online ? "bg-ok" : "bg-muted-foreground")}
        />
      </span>
      {t(online ? "statusBadge.online" : "statusBadge.offline")}
    </Badge>
  )
}

import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

export function StatusBadge({ online }: { online: boolean }) {
  return (
    <Badge
      variant={online ? "default" : "outline"}
      className={cn(
        "gap-2",
        online && "border-emerald-500/25 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
      )}
    >
      <span aria-hidden="true" className="relative inline-flex size-2">
        {online ? (
          <span className="absolute inset-0 animate-[beacon_2.4s_cubic-bezier(0.22,1,0.36,1)_infinite] rounded-full bg-emerald-500" />
        ) : null}
        <span
          className={cn(
            "relative size-2 rounded-full",
            online ? "bg-emerald-500" : "bg-muted-foreground",
          )}
        />
      </span>
      {online ? "Online" : "Offline"}
    </Badge>
  )
}

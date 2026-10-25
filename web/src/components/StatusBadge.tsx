import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

export function StatusBadge({ online }: { online: boolean }) {
  return (
    <Badge
      variant={online ? "default" : "outline"}
      className={cn(
        "gap-1.5",
        online && "border-emerald-500/25 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
      )}
    >
      <span
        aria-hidden="true"
        className={cn(
          "size-1.5 rounded-full",
          online
            ? "animate-[beacon_2.4s_ease-out_infinite] bg-emerald-500"
            : "bg-muted-foreground/60",
        )}
      />
      {online ? "Online" : "Offline"}
    </Badge>
  )
}

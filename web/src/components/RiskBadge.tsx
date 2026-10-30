import type { Risk } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

/** Why an open port deserves a look. The text is the rule's own sentence. */
export function RiskBadge({ risk }: { risk: Risk }) {
  const high = risk.severity === "high"
  return (
    <Badge
      variant="outline"
      title={risk.message}
      aria-label={`${risk.severity} risk: ${risk.message}`}
      className={cn(
        high
          ? "border-red-500/25 bg-red-500/10 text-red-700 dark:text-red-300"
          : "border-amber-500/25 bg-amber-500/10 text-amber-700 dark:text-amber-300",
      )}
    >
      {risk.severity} risk
    </Badge>
  )
}

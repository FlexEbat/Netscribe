import type { Risk } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import { t } from "@/i18n"
import { serverText } from "@/i18n/server"

/** Why an open port deserves a look. The text is the rule's own sentence. */
export function RiskBadge({ risk }: { risk: Risk }) {
  const high = risk.severity === "high"
  const severity = t(high ? "riskBadge.high" : "riskBadge.medium")
  const message = serverText(risk.message)
  return (
    <Badge
      variant="outline"
      title={message}
      aria-label={t("riskBadge.label", { severity, message })}
      className={cn(
        high
          ? "border-red-500/25 bg-red-500/10 text-red-700 dark:text-red-300"
          : "border-amber-500/25 bg-amber-500/10 text-amber-700 dark:text-amber-300",
      )}
    >
      {severity}
    </Badge>
  )
}

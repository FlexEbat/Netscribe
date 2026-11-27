import type { Risk } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import { hasKey, t, type Key } from "@/i18n"
import { serverText } from "@/i18n/server"

/** Why an open service deserves a look. The text comes from the dictionary by the rule's code; the server's sentence is the fallback. */
export function RiskBadge({ risk }: { risk: Risk }) {
  const high = risk.severity === "high"
  const severity = t(high ? "riskBadge.high" : "riskBadge.medium")
  const noteKey = `risk.${risk.code}`
  const whyKey = `riskWhy.${risk.code}`
  const message = hasKey(noteKey) ? t(noteKey as Key) : serverText(risk.message)
  const why = hasKey(whyKey) ? t(whyKey as Key) : serverText(risk.why)
  return (
    <Badge
      variant="outline"
      title={why ? `${message}. ${why}` : message}
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

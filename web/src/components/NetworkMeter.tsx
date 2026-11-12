import type { Overview } from "@/api/client"
import { t } from "@/i18n"

/** The card at the foot of the menu: how much of the network answers, as a bar. */
export function NetworkMeter({ overview }: { overview: Overview | null }) {
  if (!overview || overview.devices.total === 0) return null
  const { online, total } = overview.devices
  return (
    <div className="surface flex flex-col gap-2 rounded-2xl px-4 py-2">
      <div className="flex items-baseline justify-between text-xs text-muted-foreground">
        <span className="font-medium tracking-wider uppercase">{t("overview.online")}</span>
        <span className="readout text-foreground">
          {online} / {total}
        </span>
      </div>
      <div
        role="progressbar"
        aria-label={t("overview.online")}
        aria-valuemin={0}
        aria-valuemax={total}
        aria-valuenow={online}
        className="h-1.5 w-full overflow-hidden rounded-full bg-muted"
      >
        <div
          className="h-full w-full origin-left rounded-full bg-ok transition-transform duration-700"
          style={{ transform: `scaleX(${online / total})` }}
        />
      </div>
    </div>
  )
}

import type { Overview } from "@/api/client"
import { t } from "@/i18n"

/** The readouts of the network at the right of the top bar: a colored figure over a caption. */
export function HeaderStats({ overview }: { overview: Overview | null }) {
  if (!overview) return null
  const { devices } = overview
  const items = [
    { caption: t("overview.devices"), value: devices.total, tone: "text-foreground" },
    { caption: t("overview.online"), value: devices.online, tone: "text-ok" },
    { caption: t("overview.offline"), value: devices.offline, tone: "text-muted-foreground" },
    { caption: t("overview.newThisWeek"), value: devices.newThisWeek, tone: "text-brand" },
  ]
  return (
    <dl className="hidden items-center lg:flex">
      {items.map((item) => (
        <div key={item.caption} className="stat">
          <dd className={`stat-figure ${item.tone}`}>{item.value}</dd>
          <dt className="stat-caption">{item.caption}</dt>
        </div>
      ))}
    </dl>
  )
}

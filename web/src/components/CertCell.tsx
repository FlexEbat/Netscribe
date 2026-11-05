import { useState } from "react"
import type { TlsCert } from "@/api/client"
import { formatDate, t } from "@/i18n"

const soon = 14 * 24 * 60 * 60 * 1000

/** What a service showed in its TLS handshake: who it says it is, who vouched, and for how long. */
export function CertCell({ cert }: { cert: TlsCert | null }) {
  const [now] = useState(() => Date.now()) // read once, so a render stays the same for the same data
  if (!cert) return <span className="text-muted-foreground">–</span>
  const end = new Date(cert.notAfter).getTime()
  const left = end - now
  const expired = left <= 0
  const ending = !expired && left < soon
  const when = formatDate(cert.notAfter)
  const state = expired
    ? t("certCell.expired", { date: when })
    : ending
      ? t("certCell.endsSoon", { date: when })
      : t("certCell.validUntil", { date: when })
  return (
    <div className="flex max-w-64 flex-col gap-0.5 break-words text-xs">
      <span className="font-medium">{cert.subject || "–"}</span>
      {cert.issuer ? (
        <span className="text-muted-foreground">{t("certCell.issuer", { name: cert.issuer })}</span>
      ) : null}
      {cert.sans.length > 0 ? (
        <span className="text-muted-foreground">
          {t("certCell.alsoFor", { names: cert.sans.join(", ") })}
        </span>
      ) : null}
      <span
        className={
          expired
            ? "text-red-700 dark:text-red-400"
            : ending
              ? "text-amber-700 dark:text-amber-400"
              : "text-muted-foreground"
        }
      >
        {state}
      </span>
    </div>
  )
}

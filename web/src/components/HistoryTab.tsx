import { useEffect, useState } from "react"
import { getDeviceHistory, type DeviceHistory } from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { formatDateTime, t } from "@/i18n"
import { tRich } from "@/i18n/rich"

type State =
  { status: "loading" } | { status: "failed" } | { status: "ready"; history: DeviceHistory }

/** How often the stored scans saw the device, where it lived and when it was away. */
export function HistoryTab({ deviceId }: { deviceId: number }) {
  const [state, setState] = useState<State>({ status: "loading" })

  useEffect(() => {
    let cancelled = false
    getDeviceHistory(deviceId)
      .then((history) => !cancelled && setState({ status: "ready", history }))
      .catch(() => !cancelled && setState({ status: "failed" }))
    return () => {
      cancelled = true
    }
  }, [deviceId])

  if (state.status === "loading") {
    return (
      <Skeleton
        role="status"
        aria-label={t("historyTab.loadingTheHistory")}
        className="h-24 w-full"
      />
    )
  }
  if (state.status === "failed") {
    return <Alert variant="destructive">{t("historyTab.theHistoryCouldNotBe")}</Alert>
  }
  const h = state.history
  if (h.scans === 0) {
    return <p className="text-sm text-muted-foreground">{t("historyTab.noScanHasSeenThis")}</p>
  }
  return (
    <div className="flex flex-col gap-4 text-sm">
      <p>
        {tRich(
          "historyTab.summary",
          { uptime: <span className="text-lg font-semibold">{h.uptime}%</span> },
          { online: h.online, n: h.scans },
        )}
      </p>
      <section aria-label={t("historyTab.addresses")}>
        <h3 className="mb-1 font-medium">{t("historyTab.addresses")}</h3>
        <ul className="flex flex-col gap-1">
          {h.ips.map((a) => (
            <li key={`${a.ip}-${a.from}`}>
              <span className="font-mono">{a.ip}</span>{" "}
              <span className="text-muted-foreground">
                {formatDateTime(a.from)}
                {a.to !== a.from ? t("historyTab.untilDate", { date: formatDateTime(a.to) }) : ""}
              </span>
            </li>
          ))}
        </ul>
      </section>
      <section aria-label={t("historyTab.outages")}>
        <h3 className="mb-1 font-medium">{t("historyTab.outages")}</h3>
        {h.outages.length === 0 ? (
          <p className="text-muted-foreground">{t("historyTab.noScanHasMissedThis")}</p>
        ) : (
          <ul className="flex flex-col gap-1">
            {h.outages.map((o) => (
              <li key={o.from}>
                {o.to === null ? (
                  <>
                    {tRich(
                      "historyTab.awaySince",
                      { still: <span className="font-medium">{t("historyTab.stillAway")}</span> },
                      { date: formatDateTime(o.from) },
                    )}
                  </>
                ) : (
                  <>
                    {t("historyTab.awayFromUntil", {
                      from: formatDateTime(o.from),
                      to: formatDateTime(o.to),
                    })}
                  </>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}

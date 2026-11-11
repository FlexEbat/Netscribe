import { useState } from "react"
import { ApiError, type BulkChange, type DeviceKind, type Group } from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { kindLabels } from "@/lib/topology"
import { t } from "@/i18n"

const selectClass =
  "h-9 rounded-sm border bg-input px-3 text-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
const kinds = Object.keys(kindLabels) as DeviceKind[]

/** Changes the selected devices together. Every control applies at once and clears the choice. */
export function BulkBar({
  count,
  groups,
  onApply,
  onClear,
}: {
  count: number
  groups: Group[]
  onApply: (change: BulkChange) => Promise<void>
  onClear: () => void
}) {
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function apply(change: BulkChange) {
    setError("")
    setBusy(true)
    try {
      await onApply(change)
    } catch (e) {
      setError(e instanceof ApiError && e.status !== 0 ? e.message : t("bulkBar.couldNotChange"))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div
      role="region"
      aria-label={t("bulkBar.selectedDevices")}
      className="flex flex-col gap-2 rounded-sm border bg-card p-3"
    >
      <div className="flex flex-wrap items-center gap-3">
        <span className="text-sm font-medium">{t("bulkBar.selected", { n: count })}</span>
        <Button
          variant="outline"
          size="sm"
          disabled={busy}
          onClick={() => void apply({ trusted: true })}
        >
          {t("bulkBar.markAsTrusted")}
        </Button>
        <select
          aria-label={t("bulkBar.setTheGroupOfThe")}
          className={selectClass}
          disabled={busy}
          value=""
          onChange={(e) => e.target.value !== "" && void apply({ groupId: Number(e.target.value) })}
        >
          <option value="">{t("bulkBar.setGroup")}</option>
          <option value="0">{t("bulkBar.noGroup")}</option>
          {groups.map((g) => (
            <option key={g.id} value={g.id}>
              {g.name}
            </option>
          ))}
        </select>
        <select
          aria-label={t("bulkBar.setTheKindOfThe")}
          className={selectClass}
          disabled={busy}
          value=""
          onChange={(e) =>
            e.target.value !== "" && void apply({ kind: e.target.value as DeviceKind })
          }
        >
          <option value="">{t("bulkBar.setKind")}</option>
          {kinds.map((k) => (
            <option key={k} value={k}>
              {kindLabels[k]}
            </option>
          ))}
        </select>
        <Button variant="ghost" size="sm" onClick={onClear}>
          {t("bulkBar.clearSelection")}
        </Button>
      </div>
      {error ? <Alert variant="destructive">{error}</Alert> : null}
    </div>
  )
}

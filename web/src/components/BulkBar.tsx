import { useState } from "react"
import { ApiError, type BulkChange, type DeviceKind, type Group } from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { kindLabels } from "@/lib/topology"

const selectClass =
  "h-9 rounded-lg border bg-input px-3 text-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
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
      setError(
        e instanceof ApiError && e.status !== 0
          ? e.message
          : "The devices could not be changed. Try again later.",
      )
    } finally {
      setBusy(false)
    }
  }

  return (
    <div
      role="region"
      aria-label="Selected devices"
      className="flex flex-col gap-2 rounded-lg border bg-card p-3"
    >
      <div className="flex flex-wrap items-center gap-3">
        <span className="text-sm font-medium">
          {count} {count === 1 ? "device" : "devices"} selected
        </span>
        <Button
          variant="outline"
          size="sm"
          disabled={busy}
          onClick={() => void apply({ trusted: true })}
        >
          Mark as trusted
        </Button>
        <select
          aria-label="Set the group of the selection"
          className={selectClass}
          disabled={busy}
          value=""
          onChange={(e) => e.target.value !== "" && void apply({ groupId: Number(e.target.value) })}
        >
          <option value="">Set group…</option>
          <option value="0">No group</option>
          {groups.map((g) => (
            <option key={g.id} value={g.id}>
              {g.name}
            </option>
          ))}
        </select>
        <select
          aria-label="Set the kind of the selection"
          className={selectClass}
          disabled={busy}
          value=""
          onChange={(e) =>
            e.target.value !== "" && void apply({ kind: e.target.value as DeviceKind })
          }
        >
          <option value="">Set kind…</option>
          {kinds.map((k) => (
            <option key={k} value={k}>
              {kindLabels[k]}
            </option>
          ))}
        </select>
        <Button variant="ghost" size="sm" onClick={onClear}>
          Clear selection
        </Button>
      </div>
      {error ? <Alert variant="destructive">{error}</Alert> : null}
    </div>
  )
}

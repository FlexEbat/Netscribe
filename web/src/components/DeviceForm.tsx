import { useId, useState } from "react"
import { ApiError, type DeviceKind } from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { kindLabels } from "@/lib/topology"

export interface DeviceFormValues {
  label: string
  hostname: string
  notes: string
  kind: DeviceKind
  ip: string
  mac: string
}

interface DeviceFormProps {
  /** In edit mode the address cannot be changed: it belongs to the device that scans find. */
  mode: "create" | "edit"
  initial?: Partial<DeviceFormValues>
  submitLabel: string
  onSubmit: (values: DeviceFormValues) => Promise<void>
  onCancel: () => void
  /** Shown when the save failed because someone else changed the device. */
  onReload?: () => void
}

const kinds = Object.keys(kindLabels) as DeviceKind[]
const maxLabel = 100
const maxNotes = 2000

export function DeviceForm({
  mode,
  initial,
  submitLabel,
  onSubmit,
  onCancel,
  onReload,
}: DeviceFormProps) {
  const id = useId()
  const [values, setValues] = useState<DeviceFormValues>({
    label: initial?.label ?? "",
    hostname: initial?.hostname ?? "",
    notes: initial?.notes ?? "",
    kind: initial?.kind ?? "unknown",
    ip: initial?.ip ?? "",
    mac: initial?.mac ?? "",
  })
  const [error, setError] = useState("")
  const [conflict, setConflict] = useState(false)
  const [saving, setSaving] = useState(false)

  const set = <K extends keyof DeviceFormValues>(key: K, value: DeviceFormValues[K]) =>
    setValues((v) => ({ ...v, [key]: value }))

  async function submit(event: React.SyntheticEvent) {
    event.preventDefault()
    setError("")
    setConflict(false)
    setSaving(true)
    try {
      await onSubmit(values)
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        setConflict(true)
        setError(
          mode === "edit"
            ? "Someone else changed this device while you were editing it."
            : "A device with this address already exists.",
        )
      } else if (e instanceof ApiError && e.status === 400) {
        setError(e.message)
      } else {
        setError("The device could not be saved. Try again later.")
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-4" noValidate>
      {error ? (
        <Alert variant="destructive">
          <span>{error}</span>
          {conflict && mode === "edit" && onReload ? (
            <Button type="button" variant="outline" size="sm" className="mt-2" onClick={onReload}>
              Load the current data
            </Button>
          ) : null}
        </Alert>
      ) : null}
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={`${id}-label`}>Name</Label>
        <Input
          id={`${id}-label`}
          value={values.label}
          maxLength={maxLabel}
          onChange={(e) => set("label", e.target.value)}
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={`${id}-kind`}>Kind</Label>
        <Select value={values.kind} onValueChange={(v) => set("kind", v as DeviceKind)}>
          <SelectTrigger id={`${id}-kind`}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {kinds.map((k) => (
              <SelectItem key={k} value={k}>
                {kindLabels[k]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      {mode === "create" ? (
        <div className="flex flex-col gap-1.5">
          <Label htmlFor={`${id}-hostname`}>Hostname</Label>
          <Input
            id={`${id}-hostname`}
            value={values.hostname}
            maxLength={255}
            placeholder="optional"
            onChange={(e) => set("hostname", e.target.value)}
          />
        </div>
      ) : null}
      {mode === "create" ? (
        <div className="grid grid-cols-2 gap-3">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={`${id}-ip`}>IP address</Label>
            <Input
              id={`${id}-ip`}
              value={values.ip}
              inputMode="decimal"
              placeholder="optional"
              onChange={(e) => set("ip", e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={`${id}-mac`}>MAC address</Label>
            <Input
              id={`${id}-mac`}
              value={values.mac}
              placeholder="optional"
              onChange={(e) => set("mac", e.target.value)}
            />
          </div>
        </div>
      ) : null}
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={`${id}-notes`}>Notes</Label>
        <Textarea
          id={`${id}-notes`}
          value={values.notes}
          maxLength={maxNotes}
          onChange={(e) => set("notes", e.target.value)}
        />
      </div>
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={saving}>
          {saving ? "Saving…" : submitLabel}
        </Button>
      </div>
    </form>
  )
}

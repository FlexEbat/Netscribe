import { useId, useState } from "react"
import { ApiError, createLink } from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

interface PortsDialogProps {
  aDeviceId: number
  aName: string
  bDeviceId: number
  bName: string
  onDone: () => void
  onCancel: () => void
}

/** Asks for the port names of a link a person has just drawn, then stores the link. */
export function PortsDialog({
  aDeviceId,
  aName,
  bDeviceId,
  bName,
  onDone,
  onCancel,
}: PortsDialogProps) {
  const id = useId()
  const [aPort, setAPort] = useState("")
  const [bPort, setBPort] = useState("")
  const [error, setError] = useState("")
  const [saving, setSaving] = useState(false)

  async function submit(event: React.SyntheticEvent) {
    event.preventDefault()
    setError("")
    setSaving(true)
    try {
      await createLink({ aDeviceId, aPort: aPort.trim(), bDeviceId, bPort: bPort.trim() })
      onDone()
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) setError("This link already exists.")
      else if (e instanceof ApiError && e.status === 400) setError(e.message)
      else setError("The link could not be saved. Try again later.")
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onCancel()}>
      <DialogContent aria-describedby={undefined}>
        <DialogTitle className="text-lg font-semibold">New link</DialogTitle>
        <DialogDescription className="text-sm text-muted-foreground">
          {aName} to {bName}. The port names are optional.
        </DialogDescription>
        <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-4" noValidate>
          {error ? <Alert variant="destructive">{error}</Alert> : null}
          <div className="flex flex-col gap-2">
            <Label htmlFor={`${id}-a`}>Port on {aName}</Label>
            <Input
              id={`${id}-a`}
              value={aPort}
              maxLength={64}
              onChange={(e) => setAPort(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor={`${id}-b`}>Port on {bName}</Label>
            <Input
              id={`${id}-b`}
              value={bPort}
              maxLength={64}
              onChange={(e) => setBPort(e.target.value)}
            />
          </div>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onCancel}>
              Cancel
            </Button>
            <Button type="submit" disabled={saving}>
              {saving ? "Saving…" : "Create link"}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}

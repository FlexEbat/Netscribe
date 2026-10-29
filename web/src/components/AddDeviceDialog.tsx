import { Plus } from "@phosphor-icons/react"
import { useState } from "react"
import { createDevice, type Device } from "@/api/client"
import { DeviceForm } from "@/components/DeviceForm"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"

interface AddDeviceDialogProps {
  onCreated: (device: Device) => void
}

/** A button that opens the form for a device the network does not show by itself. */
export function AddDeviceDialog({ onCreated }: AddDeviceDialogProps) {
  const [open, setOpen] = useState(false)
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm">
          <Plus className="size-4" aria-hidden="true" />
          Add device
        </Button>
      </DialogTrigger>
      <DialogContent aria-describedby={undefined}>
        <DialogTitle className="text-lg font-semibold">Add a device</DialogTitle>
        <DialogDescription className="text-sm text-muted-foreground">
          For something the scans cannot see: an unmanaged switch, a patch panel, a device at
          another site. Give it a name, an address, or both.
        </DialogDescription>
        <DeviceForm
          mode="create"
          submitLabel="Add device"
          onCancel={() => setOpen(false)}
          onSubmit={async (values) => {
            const device = await createDevice(values)
            setOpen(false)
            onCreated(device)
          }}
        />
      </DialogContent>
    </Dialog>
  )
}

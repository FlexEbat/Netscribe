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
import { t } from "@/i18n"

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
          {t("addDeviceDialog.addDevice")}
        </Button>
      </DialogTrigger>
      <DialogContent aria-describedby={undefined}>
        <DialogTitle className="text-lg font-semibold">
          {t("addDeviceDialog.addADevice")}
        </DialogTitle>
        <DialogDescription className="text-sm text-muted-foreground">
          {t("addDeviceDialog.forSomethingTheScansCannot")}
        </DialogDescription>
        <DeviceForm
          mode="create"
          submitLabel={t("addDeviceDialog.addDevice")}
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

import { Trash } from "@phosphor-icons/react"
import { useState } from "react"
import { Button } from "@/components/ui/button"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { t } from "@/i18n"

interface DeleteDeviceButtonProps {
  name: string
  onConfirm: () => Promise<void>
}

export function DeleteDeviceButton({ name, onConfirm }: DeleteDeviceButtonProps) {
  const [open, setOpen] = useState(false)
  const [failed, setFailed] = useState(false)
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        <Trash className="size-4" aria-hidden="true" />
        {t("deleteDeviceButton.delete")}
      </Button>
      <AlertDialog open={open} onOpenChange={setOpen}>
        <AlertDialogContent>
          <AlertDialogTitle className="text-lg font-semibold">
            {t("deleteDeviceButton.deleteName", { name })}
          </AlertDialogTitle>
          <AlertDialogDescription className="text-sm text-muted-foreground">
            {t("deleteDeviceButton.theDeviceAndItsLinks")}
          </AlertDialogDescription>
          {failed ? (
            <p role="alert" className="text-sm text-destructive">
              {t("deleteDeviceButton.theDeviceCouldNotBe")}
            </p>
          ) : null}
          <div className="flex justify-end gap-2">
            <AlertDialogCancel>{t("deleteDeviceButton.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault()
                setFailed(false)
                onConfirm().then(
                  () => setOpen(false),
                  () => setFailed(true),
                )
              }}
            >
              {t("deleteDeviceButton.delete")}
            </AlertDialogAction>
          </div>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

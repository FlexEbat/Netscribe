import { useState } from "react"
import { Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"

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
        <Trash2 className="size-4" aria-hidden="true" />
        Delete
      </Button>
      <AlertDialog open={open} onOpenChange={setOpen}>
        <AlertDialogContent>
          <AlertDialogTitle className="text-lg font-semibold">Delete {name}?</AlertDialogTitle>
          <AlertDialogDescription className="text-sm text-muted-foreground">
            The device and its links are removed. A scan that sees the device again adds it back.
          </AlertDialogDescription>
          {failed ? (
            <p role="alert" className="text-sm text-destructive">
              The device could not be deleted.
            </p>
          ) : null}
          <div className="flex justify-end gap-2">
            <AlertDialogCancel>Cancel</AlertDialogCancel>
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
              Delete
            </AlertDialogAction>
          </div>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

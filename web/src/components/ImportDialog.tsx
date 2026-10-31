import { UploadSimple } from "@phosphor-icons/react"
import { useId, useState } from "react"
import { ApiError, importDevices, type ImportProblem } from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"

type Checked =
  | { status: "idle" }
  | { status: "checking" }
  | { status: "ok"; csv: string; count: number }
  | { status: "bad"; message: string; rows: ImportProblem[] }

/** Imports devices from a CSV file. The file is checked first and nothing is created until
 * the person confirms. */
export function ImportDialog({ onImported }: { onImported: (count: number) => void }) {
  const id = useId()
  const [open, setOpen] = useState(false)
  const [checked, setChecked] = useState<Checked>({ status: "idle" })
  const [importing, setImporting] = useState(false)

  function reset(next: boolean) {
    setOpen(next)
    setChecked({ status: "idle" })
  }

  async function choose(file: File | undefined) {
    if (!file) return
    setChecked({ status: "checking" })
    try {
      const csv = await file.text()
      const count = await importDevices(csv, true)
      setChecked({ status: "ok", csv, count })
    } catch (e) {
      setChecked(
        e instanceof ApiError && e.status !== 0
          ? { status: "bad", message: e.message, rows: e.rows }
          : { status: "bad", message: "The file could not be checked. Try again later.", rows: [] },
      )
    }
  }

  async function run() {
    if (checked.status !== "ok") return
    setImporting(true)
    try {
      const count = await importDevices(checked.csv, false)
      reset(false)
      onImported(count)
    } catch (e) {
      setChecked(
        e instanceof ApiError && e.status !== 0
          ? { status: "bad", message: e.message, rows: e.rows }
          : { status: "bad", message: "The import failed. Nothing was created.", rows: [] },
      )
    } finally {
      setImporting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={reset}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm">
          <UploadSimple className="size-4" aria-hidden="true" />
          Import CSV
        </Button>
      </DialogTrigger>
      <DialogContent aria-describedby={undefined}>
        <DialogTitle className="text-lg font-semibold">Import devices from a CSV file</DialogTitle>
        <DialogDescription className="text-sm text-muted-foreground">
          The first line names the columns: label, ip, mac, hostname, kind, group, notes. A row
          needs a label, an ip or a mac. The group must exist. Either every row is imported or none.
        </DialogDescription>
        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${id}-file`}>CSV file</Label>
            <input
              id={`${id}-file`}
              type="file"
              accept=".csv,text/csv"
              className="text-sm"
              onChange={(e) => void choose(e.target.files?.[0])}
            />
          </div>
          {checked.status === "checking" ? (
            <p role="status" className="text-sm text-muted-foreground">
              Checking the file
            </p>
          ) : null}
          {checked.status === "ok" ? (
            <p role="status" className="text-sm">
              The file is fine. {checked.count} {checked.count === 1 ? "device" : "devices"} will be
              created.
            </p>
          ) : null}
          {checked.status === "bad" ? (
            <Alert variant="destructive">
              <p>{checked.message}</p>
              {checked.rows.length > 0 ? (
                <ul className="mt-2 list-disc pl-4 text-sm">
                  {checked.rows.map((r) => (
                    <li key={`${r.line}-${r.error}`}>
                      Line {r.line}: {r.error}
                    </li>
                  ))}
                </ul>
              ) : null}
            </Alert>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => reset(false)}>
              Cancel
            </Button>
            <Button disabled={checked.status !== "ok" || importing} onClick={() => void run()}>
              {checked.status === "ok"
                ? `Import ${checked.count} ${checked.count === 1 ? "device" : "devices"}`
                : "Import"}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

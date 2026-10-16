import { useState } from "react"
import { Download } from "lucide-react"
import { downloadExport } from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

export type ExportFormat = "md" | "html" | "pdf" | "drawio"

const labels: Record<ExportFormat, string> = {
  md: "Markdown",
  html: "HTML",
  pdf: "PDF",
  drawio: "Draw.io",
}

interface ExportMenuProps {
  formats: ExportFormat[]
}

function save(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement("a")
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

/**
 * A menu of downloads. A file is fetched first and saved only when the server sends one:
 * a PDF the server cannot print is a message on the page, not a file named "pdf".
 */
export function ExportMenu({ formats }: ExportMenuProps) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  async function run(format: ExportFormat) {
    setBusy(true)
    setError("")
    try {
      const { blob, filename } = await downloadExport(format)
      save(blob, filename)
    } catch (e) {
      setError(e instanceof Error ? e.message : "The export failed")
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex flex-col items-end gap-2">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button size="sm" variant="outline" disabled={busy}>
            <Download aria-hidden />
            {busy ? "Preparing…" : "Export"}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          {formats.map((f) => (
            <DropdownMenuItem key={f} onSelect={() => void run(f)}>
              {labels[f]}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
      {error ? (
        <Alert variant="destructive" className="max-w-sm text-xs">
          {error}
        </Alert>
      ) : null}
    </div>
  )
}

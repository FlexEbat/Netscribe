import { Download } from "lucide-react"
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

/**
 * A menu of downloads. Each item is a plain link to the export route: the browser sends
 * the session cookie and the server answers with an attachment.
 */
export function ExportMenu({ formats }: ExportMenuProps) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="sm" variant="outline">
          <Download aria-hidden />
          Export
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {formats.map((f) => (
          <DropdownMenuItem key={f} asChild>
            <a href={`/api/export/${f}`} download>
              {labels[f]}
            </a>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

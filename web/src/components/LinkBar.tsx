import { useState } from "react"
import { Unlink } from "lucide-react"
import { ApiError, deleteLink, type Link } from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"

interface LinkBarProps {
  link: Link
  aName: string
  bName: string
  canEdit: boolean
  onDeleted: () => void
}

/** Shows the selected link, and lets a person with the right delete one drawn by hand. */
export function LinkBar({ link, aName, bName, canEdit, onDeleted }: LinkBarProps) {
  const [error, setError] = useState("")
  const manual = link.source === "manual"

  async function remove() {
    setError("")
    try {
      await deleteLink(link.id)
      onDeleted()
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        setError("This link was found by a scan. It disappears when the scan no longer sees it.")
      } else {
        setError("The link could not be deleted. Try again later.")
      }
    }
  }

  return (
    <div className="surface flex flex-wrap items-center gap-3 px-4 py-2 text-sm">
      <span>
        Link: {aName}
        {link.aPort ? ` (${link.aPort})` : ""} to {bName}
        {link.bPort ? ` (${link.bPort})` : ""},{" "}
        {manual ? "drawn by hand" : `found by ${link.source}`}
      </span>
      {canEdit && manual ? (
        <Button variant="outline" size="sm" onClick={() => void remove()}>
          <Unlink className="size-4" aria-hidden="true" />
          Delete link
        </Button>
      ) : null}
      {error ? <Alert variant="destructive">{error}</Alert> : null}
    </div>
  )
}

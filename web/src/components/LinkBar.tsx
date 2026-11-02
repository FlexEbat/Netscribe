import { LinkBreak } from "@phosphor-icons/react"
import { useState } from "react"
import { ApiError, deleteLink, type Link } from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { t } from "@/i18n"

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
        setError(t("linkBar.foundByScan"))
      } else {
        setError(t("linkBar.couldNotDelete"))
      }
    }
  }

  return (
    <div className="surface flex flex-wrap items-center gap-3 px-4 py-2 text-sm">
      <span>
        {t(manual ? "linkBar.drawn" : "linkBar.found", {
          a: link.aPort ? `${aName} (${link.aPort})` : aName,
          b: link.bPort ? `${bName} (${link.bPort})` : bName,
          source: link.source,
        })}
      </span>
      {canEdit && manual ? (
        <Button variant="outline" size="sm" onClick={() => void remove()}>
          <LinkBreak className="size-4" aria-hidden="true" />
          {t("linkBar.deleteLink")}
        </Button>
      ) : null}
      {error ? <Alert variant="destructive">{error}</Alert> : null}
    </div>
  )
}

import { BookmarkSimple } from "@phosphor-icons/react"
import { useCallback, useEffect, useId, useState } from "react"
import {
  ApiError,
  createFilter,
  deleteFilter,
  listFilters,
  type FilterPage,
  type SavedFilter,
} from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { t } from "@/i18n"

function reason(error: unknown, fallback: string): string {
  return error instanceof ApiError && error.status !== 0 ? error.message : fallback
}

/** Saves the filter that a page shows now, and applies or deletes the ones saved before.
 * `current` is the filter as a query string, empty when nothing is filtered. */
export function SavedFilters({
  page,
  current,
  onApply,
}: {
  page: FilterPage
  current: string
  onApply: (query: string) => void
}) {
  const id = useId()
  const [open, setOpen] = useState(false)
  const [filters, setFilters] = useState<SavedFilter[] | null>(null)
  const [name, setName] = useState("")
  const [error, setError] = useState("")

  const load = useCallback(() => {
    listFilters(page).then(setFilters, (e) => {
      setFilters([])
      setError(reason(e, t("savedFilters.couldNotLoad")))
    })
  }, [page])

  useEffect(() => {
    if (open) load()
  }, [open, load])

  async function save() {
    setError("")
    try {
      await createFilter(page, name, current)
      setName("")
      load()
    } catch (e) {
      setError(reason(e, t("savedFilters.couldNotSave")))
    }
  }

  async function remove(f: SavedFilter) {
    setError("")
    try {
      await deleteFilter(f.id)
      load()
    } catch (e) {
      setError(reason(e, t("savedFilters.couldNotDelete")))
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm">
          <BookmarkSimple className="size-4" aria-hidden="true" />
          {t("savedFilters.savedFilters")}
        </Button>
      </DialogTrigger>
      <DialogContent aria-describedby={undefined}>
        <DialogTitle className="text-lg font-semibold">
          {t("savedFilters.savedFilters")}
        </DialogTitle>
        <DialogDescription className="text-sm text-muted-foreground">
          {t("savedFilters.keepTheFilterYouUse")}
        </DialogDescription>
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${id}-name`}>{t("savedFilters.nameForTheCurrentFilter")}</Label>
            <div className="flex gap-2">
              <Input
                id={`${id}-name`}
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={current === ""}
              />
              <Button disabled={current === "" || name.trim() === ""} onClick={() => void save()}>
                {t("savedFilters.saveFilter")}
              </Button>
            </div>
            {current === "" ? (
              <p className="text-xs text-muted-foreground">{t("savedFilters.setASearchOrA")}</p>
            ) : null}
          </div>
          {error ? <Alert variant="destructive">{error}</Alert> : null}
          {filters === null ? (
            <p role="status" className="text-sm text-muted-foreground">
              {t("savedFilters.loadingTheSavedFilters")}
            </p>
          ) : filters.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("savedFilters.noFilterIsSavedYet")}</p>
          ) : (
            <ul className="flex flex-col gap-2">
              {filters.map((f) => (
                <li key={f.id} className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{f.name}</span>
                  <span className="font-mono text-xs text-muted-foreground">{f.query}</span>
                  <span className="ml-auto flex gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      aria-label={t("savedFilters.applyName", { name: f.name })}
                      onClick={() => {
                        onApply(f.query)
                        setOpen(false)
                      }}
                    >
                      {t("savedFilters.apply")}
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      aria-label={t("savedFilters.deleteName", { name: f.name })}
                      onClick={() => void remove(f)}
                    >
                      {t("savedFilters.delete")}
                    </Button>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}

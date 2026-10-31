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
      setError(reason(e, "The saved filters could not be loaded."))
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
      setError(reason(e, "The filter could not be saved. Try again later."))
    }
  }

  async function remove(f: SavedFilter) {
    setError("")
    try {
      await deleteFilter(f.id)
      load()
    } catch (e) {
      setError(reason(e, "The filter could not be deleted. Try again later."))
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm">
          <BookmarkSimple className="size-4" aria-hidden="true" />
          Saved filters
        </Button>
      </DialogTrigger>
      <DialogContent aria-describedby={undefined}>
        <DialogTitle className="text-lg font-semibold">Saved filters</DialogTitle>
        <DialogDescription className="text-sm text-muted-foreground">
          Keep the filter you use often and apply it again with one click. Only you see your
          filters.
        </DialogDescription>
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${id}-name`}>Name for the current filter</Label>
            <div className="flex gap-2">
              <Input
                id={`${id}-name`}
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={current === ""}
              />
              <Button disabled={current === "" || name.trim() === ""} onClick={() => void save()}>
                Save filter
              </Button>
            </div>
            {current === "" ? (
              <p className="text-xs text-muted-foreground">
                Set a search or a filter first, then save it.
              </p>
            ) : null}
          </div>
          {error ? <Alert variant="destructive">{error}</Alert> : null}
          {filters === null ? (
            <p role="status" className="text-sm text-muted-foreground">
              Loading the saved filters
            </p>
          ) : filters.length === 0 ? (
            <p className="text-sm text-muted-foreground">No filter is saved yet.</p>
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
                      aria-label={`Apply ${f.name}`}
                      onClick={() => {
                        onApply(f.query)
                        setOpen(false)
                      }}
                    >
                      Apply
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      aria-label={`Delete filter ${f.name}`}
                      onClick={() => void remove(f)}
                    >
                      Delete
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

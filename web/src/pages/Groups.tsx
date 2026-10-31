import { Plus } from "@phosphor-icons/react"
import { useCallback, useEffect, useId, useState } from "react"
import {
  ApiError,
  createGroup,
  deleteGroup,
  listGroups,
  updateGroup,
  type Group,
} from "@/api/client"
import { useSession } from "@/api/session"
import { EmptyState } from "@/components/EmptyState"
import { Alert } from "@/components/ui/alert"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { can } from "@/lib/permissions"

type State = { status: "loading" } | { status: "failed" } | { status: "ready"; groups: Group[] }
type Editing = { kind: "new" } | { kind: "edit"; group: Group } | { kind: "delete"; group: Group }

function message(error: unknown, fallback: string): string {
  return error instanceof ApiError && error.status !== 0 ? error.message : fallback
}

/** The groups of the network: a site, a VLAN, a range of addresses. */
export function Groups() {
  const { state: session } = useSession()
  const canWrite =
    session.status === "signedIn" && can(session.session.permissions, "devices:write")
  const [state, setState] = useState<State>({ status: "loading" })
  const [editing, setEditing] = useState<Editing | null>(null)
  const [refresh, setRefresh] = useState(0)

  useEffect(() => {
    let cancelled = false
    listGroups()
      .then((groups) => !cancelled && setState({ status: "ready", groups }))
      .catch(() => !cancelled && setState({ status: "failed" }))
    return () => {
      cancelled = true
    }
  }, [refresh])

  const done = useCallback(() => {
    setEditing(null)
    setRefresh((n) => n + 1)
  }, [])

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h1 className="text-2xl font-semibold">Groups</h1>
        {canWrite ? (
          <Button size="sm" onClick={() => setEditing({ kind: "new" })}>
            <Plus className="size-4" aria-hidden="true" />
            Add group
          </Button>
        ) : null}
      </div>
      <p className="text-sm text-muted-foreground">
        A group names a site, a VLAN or a range of addresses. A device is in the group that a person
        chose for it, else in the first group whose ranges contain its address.
      </p>
      {state.status === "loading" ? (
        <Skeleton role="status" aria-label="Loading the groups" className="h-24 w-full" />
      ) : null}
      {state.status === "failed" ? (
        <Alert variant="destructive">The groups could not be loaded. Try again later.</Alert>
      ) : null}
      {state.status === "ready" && state.groups.length === 0 ? (
        <EmptyState
          title="No groups yet"
          text="Add a group to sort the devices into sites or VLANs."
        />
      ) : null}
      {state.status === "ready" && state.groups.length > 0 ? (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>VLAN</TableHead>
              <TableHead>Ranges</TableHead>
              <TableHead>Devices</TableHead>
              {canWrite ? <TableHead>Actions</TableHead> : null}
            </TableRow>
          </TableHeader>
          <TableBody>
            {state.groups.map((g) => (
              <TableRow key={g.id}>
                <TableCell>
                  <span className="font-medium">{g.name}</span>
                  {g.description ? (
                    <span className="block text-xs text-muted-foreground">{g.description}</span>
                  ) : null}
                </TableCell>
                <TableCell>{g.vlan ?? "–"}</TableCell>
                <TableCell className="font-mono text-xs">
                  {g.ranges.length > 0 ? g.ranges.join(", ") : "–"}
                </TableCell>
                <TableCell>{g.deviceCount}</TableCell>
                {canWrite ? (
                  <TableCell className="flex gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      aria-label={`Edit ${g.name}`}
                      onClick={() => setEditing({ kind: "edit", group: g })}
                    >
                      Edit
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      aria-label={`Delete ${g.name}`}
                      onClick={() => setEditing({ kind: "delete", group: g })}
                    >
                      Delete
                    </Button>
                  </TableCell>
                ) : null}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : null}
      {editing?.kind === "new" ? (
        <GroupForm onClose={() => setEditing(null)} onDone={done} />
      ) : null}
      {editing?.kind === "edit" ? (
        <GroupForm group={editing.group} onClose={() => setEditing(null)} onDone={done} />
      ) : null}
      {editing?.kind === "delete" ? (
        <DeleteGroup group={editing.group} onClose={() => setEditing(null)} onDone={done} />
      ) : null}
    </div>
  )
}

function GroupForm({
  group,
  onClose,
  onDone,
}: {
  group?: Group
  onClose: () => void
  onDone: () => void
}) {
  const id = useId()
  const [name, setName] = useState(group?.name ?? "")
  const [description, setDescription] = useState(group?.description ?? "")
  const [vlan, setVlan] = useState(group?.vlan ? String(group.vlan) : "")
  const [ranges, setRanges] = useState(group?.ranges.join("\n") ?? "")
  const [error, setError] = useState("")
  const [saving, setSaving] = useState(false)

  async function submit() {
    setError("")
    const number = vlan.trim() === "" ? null : Number(vlan)
    if (number !== null && !Number.isInteger(number)) {
      setError("The VLAN must be a whole number from 1 to 4094.")
      return
    }
    const list = ranges.split(/[\s,]+/).filter((r) => r !== "")
    setSaving(true)
    try {
      if (group) {
        // On an edit, 0 removes the VLAN number.
        await updateGroup(group.id, {
          name,
          description,
          vlan: number ?? 0,
          ranges: list,
          version: group.version,
        })
      } else {
        await createGroup({ name, description, vlan: number, ranges: list })
      }
      onDone()
    } catch (e) {
      setError(message(e, "The group could not be saved. Try again later."))
      setSaving(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent aria-describedby={undefined}>
        <DialogTitle className="text-lg font-semibold">
          {group ? `Edit ${group.name}` : "Add a group"}
        </DialogTitle>
        <DialogDescription className="text-sm text-muted-foreground">
          Ranges are written as networks, one per line, for example 192.168.1.0/24.
        </DialogDescription>
        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${id}-name`}>Name</Label>
            <Input id={`${id}-name`} value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${id}-description`}>Description</Label>
            <Input
              id={`${id}-description`}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${id}-vlan`}>VLAN (optional)</Label>
            <Input
              id={`${id}-vlan`}
              inputMode="numeric"
              value={vlan}
              onChange={(e) => setVlan(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${id}-ranges`}>Ranges</Label>
            <Textarea
              id={`${id}-ranges`}
              className="font-mono"
              rows={3}
              value={ranges}
              onChange={(e) => setRanges(e.target.value)}
            />
          </div>
          {error ? <Alert variant="destructive">{error}</Alert> : null}
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button disabled={saving} onClick={() => void submit()}>
              {group ? "Save" : "Add group"}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function DeleteGroup({
  group,
  onClose,
  onDone,
}: {
  group: Group
  onClose: () => void
  onDone: () => void
}) {
  const [error, setError] = useState("")
  return (
    <AlertDialog open onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent>
        <AlertDialogTitle className="text-lg font-semibold">Delete {group.name}?</AlertDialogTitle>
        <AlertDialogDescription className="text-sm text-muted-foreground">
          The group goes away. Its devices stay and fall back to the next group whose ranges contain
          their address, or to none.
        </AlertDialogDescription>
        {error ? (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={(e) => {
              e.preventDefault()
              setError("")
              deleteGroup(group.id).then(onDone, (err) =>
                setError(message(err, "The group could not be deleted. Try again later.")),
              )
            }}
          >
            Delete
          </AlertDialogAction>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  )
}

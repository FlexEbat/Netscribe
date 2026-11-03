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
import { t } from "@/i18n"

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
        <h1 className="text-2xl font-semibold">{t("groups.groups")}</h1>
        {canWrite ? (
          <Button size="sm" onClick={() => setEditing({ kind: "new" })}>
            <Plus className="size-4" aria-hidden="true" />
            {t("groups.addGroup")}
          </Button>
        ) : null}
      </div>
      <p className="text-sm text-muted-foreground">{t("groups.aGroupNamesASite")}</p>
      {state.status === "loading" ? (
        <Skeleton role="status" aria-label={t("groups.loadingTheGroups")} className="h-24 w-full" />
      ) : null}
      {state.status === "failed" ? (
        <Alert variant="destructive">{t("groups.theGroupsCouldNotBe")}</Alert>
      ) : null}
      {state.status === "ready" && state.groups.length === 0 ? (
        <EmptyState title={t("groups.noGroupsYet")} text={t("groups.noGroupsText")} />
      ) : null}
      {state.status === "ready" && state.groups.length > 0 ? (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("groups.name")}</TableHead>
              <TableHead>{t("groups.vlan")}</TableHead>
              <TableHead>{t("groups.ranges")}</TableHead>
              <TableHead>{t("groups.devices")}</TableHead>
              {canWrite ? <TableHead>{t("groups.actions")}</TableHead> : null}
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
                      aria-label={t("groups.editName", { name: g.name })}
                      onClick={() => setEditing({ kind: "edit", group: g })}
                    >
                      {t("groups.edit")}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      aria-label={t("groups.deleteName", { name: g.name })}
                      onClick={() => setEditing({ kind: "delete", group: g })}
                    >
                      {t("groups.delete")}
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
      setError(t("groups.vlanInvalid"))
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
      setError(message(e, t("groups.couldNotSave")))
      setSaving(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent aria-describedby={undefined}>
        <DialogTitle className="text-lg font-semibold">
          {group ? t("groups.editName", { name: group.name }) : t("groups.addAGroup")}
        </DialogTitle>
        <DialogDescription className="text-sm text-muted-foreground">
          {t("groups.rangesAreWrittenAsNetworks")}
        </DialogDescription>
        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${id}-name`}>{t("groups.name")}</Label>
            <Input id={`${id}-name`} value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${id}-description`}>{t("groups.description")}</Label>
            <Input
              id={`${id}-description`}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${id}-vlan`}>{t("groups.vlanOptional")}</Label>
            <Input
              id={`${id}-vlan`}
              inputMode="numeric"
              value={vlan}
              onChange={(e) => setVlan(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${id}-ranges`}>{t("groups.ranges")}</Label>
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
              {t("groups.cancel")}
            </Button>
            <Button disabled={saving} onClick={() => void submit()}>
              {t(group ? "groups.save" : "groups.addGroup")}
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
        <AlertDialogTitle className="text-lg font-semibold">
          {t("groups.deleteGroupName", { name: group.name })}
        </AlertDialogTitle>
        <AlertDialogDescription className="text-sm text-muted-foreground">
          {t("groups.theGroupGoesAwayIts")}
        </AlertDialogDescription>
        {error ? (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <AlertDialogCancel>{t("groups.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={(e) => {
              e.preventDefault()
              setError("")
              deleteGroup(group.id).then(onDone, (err) =>
                setError(message(err, t("groups.couldNotDelete"))),
              )
            }}
          >
            {t("groups.delete")}
          </AlertDialogAction>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  )
}

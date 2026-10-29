import { useCallback, useEffect, useState } from "react"
import {
  ApiError,
  createUser,
  deleteUser,
  listUsers,
  resetPassword,
  updateUser,
} from "@/api/client"
import { RoleBadge } from "@/components/RoleBadge"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { roleLabels, type Role, type User } from "@/lib/permissions"

const roles = Object.keys(roleLabels) as Role[]
const lastAdminMessage = "The last administrator cannot be demoted, disabled or deleted."

type State = { status: "loading" } | { status: "failed" } | { status: "ready"; users: User[] }
type Dialog_ = { kind: "add" } | { kind: "password"; user: User } | { kind: "delete"; user: User }

function message(error: unknown, fallback: string): string {
  if (error instanceof ApiError && error.status === 409) {
    return error.message.includes("administrator") ? lastAdminMessage : error.message
  }
  if (error instanceof ApiError && error.status === 400) return error.message
  return fallback
}

export function Users() {
  const [state, setState] = useState<State>({ status: "loading" })
  const [dialog, setDialog] = useState<Dialog_ | null>(null)
  const [notice, setNotice] = useState("")

  const load = useCallback(() => {
    listUsers()
      .then((users) => setState({ status: "ready", users }))
      .catch(() => setState((s) => (s.status === "ready" ? s : { status: "failed" })))
  }, [])

  useEffect(() => {
    load()
  }, [load])

  async function change(user: User, changes: { role?: Role; disabled?: boolean }) {
    setNotice("")
    try {
      await updateUser(user.id, changes)
      load()
    } catch (e) {
      setNotice(message(e, "The change could not be saved. Try again later."))
    }
  }

  if (state.status === "loading") {
    return <Skeleton role="status" aria-label="Loading users" className="h-48 w-full" />
  }
  if (state.status === "failed") {
    return <Alert variant="destructive">The users could not be loaded. Try again later.</Alert>
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold">Users</h1>
        <Button size="sm" onClick={() => setDialog({ kind: "add" })}>
          Add user
        </Button>
      </div>
      {notice ? <Alert variant="destructive">{notice}</Alert> : null}
      <Table>
        <TableCaption>Users ({state.users.length})</TableCaption>
        <TableHeader>
          <TableRow>
            <TableHead>User</TableHead>
            <TableHead>Role</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Last sign in</TableHead>
            <TableHead>Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {state.users.map((u) => (
            <TableRow key={u.id}>
              <TableCell>
                <div className="font-medium">{u.username}</div>
                {u.displayName ? (
                  <div className="text-xs text-muted-foreground">{u.displayName}</div>
                ) : null}
              </TableCell>
              <TableCell>
                <div className="flex items-center gap-2">
                  <RoleBadge role={u.role} />
                  <select
                    aria-label={`Role of ${u.username}`}
                    className="h-8 rounded-md border bg-input px-2 text-xs"
                    value={u.role}
                    onChange={(e) => void change(u, { role: e.target.value as Role })}
                  >
                    {roles.map((r) => (
                      <option key={r} value={r}>
                        {roleLabels[r]}
                      </option>
                    ))}
                  </select>
                </div>
              </TableCell>
              <TableCell>
                {u.disabled ? "Disabled" : u.mustChangePassword ? "Must change password" : "Active"}
              </TableCell>
              <TableCell>
                {u.lastLoginAt ? new Date(u.lastLoginAt).toLocaleString() : "never"}
              </TableCell>
              <TableCell>
                <div className="flex flex-wrap gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => void change(u, { disabled: !u.disabled })}
                  >
                    {u.disabled ? "Enable" : "Disable"}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setDialog({ kind: "password", user: u })}
                  >
                    Reset password
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setDialog({ kind: "delete", user: u })}
                  >
                    Delete
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      {dialog?.kind === "add" ? (
        <AddUserDialog
          onClose={() => setDialog(null)}
          onDone={() => {
            setDialog(null)
            load()
          }}
        />
      ) : null}
      {dialog?.kind === "password" ? (
        <ResetPasswordDialog
          user={dialog.user}
          onClose={() => setDialog(null)}
          onDone={() => {
            setDialog(null)
            load()
          }}
        />
      ) : null}
      {dialog?.kind === "delete" ? (
        <DeleteUserDialog
          user={dialog.user}
          onClose={() => setDialog(null)}
          onDone={() => {
            setDialog(null)
            load()
          }}
        />
      ) : null}
    </div>
  )
}

function AddUserDialog({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const [username, setUsername] = useState("")
  const [role, setRole] = useState<Role>("viewer")
  const [password, setPassword] = useState("")
  const [error, setError] = useState("")
  const [saving, setSaving] = useState(false)

  async function submit(event: React.SyntheticEvent) {
    event.preventDefault()
    setError("")
    setSaving(true)
    try {
      await createUser({ username, role, password })
      onDone()
    } catch (e) {
      setError(message(e, "The user could not be created. Try again later."))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent aria-describedby={undefined}>
        <DialogTitle className="text-lg font-semibold">Add a user</DialogTitle>
        <DialogDescription className="text-sm text-muted-foreground">
          The password is temporary: the user must choose a new one at the first sign-in.
        </DialogDescription>
        <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-4" noValidate>
          {error ? <Alert variant="destructive">{error}</Alert> : null}
          <div className="flex flex-col gap-2">
            <Label htmlFor="new-username">Username</Label>
            <Input
              id="new-username"
              value={username}
              autoComplete="off"
              onChange={(e) => setUsername(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="new-role">Role</Label>
            <select
              id="new-role"
              className="h-9 rounded-lg border bg-input px-3 text-sm"
              value={role}
              onChange={(e) => setRole(e.target.value as Role)}
            >
              {roles.map((r) => (
                <option key={r} value={r}>
                  {roleLabels[r]}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="new-temp-password">Temporary password</Label>
            <Input
              id="new-temp-password"
              type="password"
              value={password}
              autoComplete="new-password"
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={saving}>
              {saving ? "Saving…" : "Create user"}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function ResetPasswordDialog({
  user,
  onClose,
  onDone,
}: {
  user: User
  onClose: () => void
  onDone: () => void
}) {
  const [password, setPassword] = useState("")
  const [error, setError] = useState("")
  const [saving, setSaving] = useState(false)

  async function submit(event: React.SyntheticEvent) {
    event.preventDefault()
    setError("")
    setSaving(true)
    try {
      await resetPassword(user.id, password)
      onDone()
    } catch (e) {
      setError(message(e, "The password could not be set. Try again later."))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent aria-describedby={undefined}>
        <DialogTitle className="text-lg font-semibold">
          Reset the password of {user.username}
        </DialogTitle>
        <DialogDescription className="text-sm text-muted-foreground">
          The user is signed out everywhere and must choose a new password at the next sign-in.
        </DialogDescription>
        <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-4" noValidate>
          {error ? <Alert variant="destructive">{error}</Alert> : null}
          <div className="flex flex-col gap-2">
            <Label htmlFor="reset-password">Temporary password</Label>
            <Input
              id="reset-password"
              type="password"
              value={password}
              autoComplete="new-password"
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={saving}>
              {saving ? "Saving…" : "Set password"}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function DeleteUserDialog({
  user,
  onClose,
  onDone,
}: {
  user: User
  onClose: () => void
  onDone: () => void
}) {
  const [error, setError] = useState("")
  return (
    <AlertDialog open onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent>
        <AlertDialogTitle className="text-lg font-semibold">
          Delete {user.username}?
        </AlertDialogTitle>
        <AlertDialogDescription className="text-sm text-muted-foreground">
          The account, its sessions and its access tokens are removed. This cannot be undone.
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
              deleteUser(user.id).then(onDone, (err) =>
                setError(message(err, "The user could not be deleted. Try again later.")),
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

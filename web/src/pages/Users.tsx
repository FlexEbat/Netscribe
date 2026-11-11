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
import { formatDateTime, t } from "@/i18n"
import { PageTitle } from "@/components/PageTitle"

const roles = Object.keys(roleLabels) as Role[]

type State = { status: "loading" } | { status: "failed" } | { status: "ready"; users: User[] }
type Dialog_ = { kind: "add" } | { kind: "password"; user: User } | { kind: "delete"; user: User }

function message(error: unknown, fallback: string): string {
  if (error instanceof ApiError && error.status === 409) {
    return error.serverMessage.includes("administrator") ? t("users.lastAdmin") : error.message
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
      setNotice(message(e, t("users.couldNotSaveChange")))
    }
  }

  if (state.status === "loading") {
    return <Skeleton role="status" aria-label={t("users.loadingUsers")} className="h-48 w-full" />
  }
  if (state.status === "failed") {
    return <Alert variant="destructive">{t("users.theUsersCouldNotBe")}</Alert>
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-3">
        <PageTitle page="users">{t("users.users")}</PageTitle>
        <Button size="sm" onClick={() => setDialog({ kind: "add" })}>
          {t("users.addUser")}
        </Button>
      </div>
      {notice ? <Alert variant="destructive">{notice}</Alert> : null}
      <Table>
        <TableCaption>{t("users.caption", { n: state.users.length })}</TableCaption>
        <TableHeader>
          <TableRow>
            <TableHead>{t("users.user")}</TableHead>
            <TableHead>{t("users.role")}</TableHead>
            <TableHead>{t("users.status")}</TableHead>
            <TableHead>{t("users.lastSignIn")}</TableHead>
            <TableHead>{t("users.actions")}</TableHead>
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
                    aria-label={t("users.roleOf", { name: u.username })}
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
                {t(
                  u.disabled
                    ? "users.disabled"
                    : u.mustChangePassword
                      ? "users.mustChange"
                      : "users.active",
                )}
              </TableCell>
              <TableCell>
                {u.lastLoginAt ? formatDateTime(u.lastLoginAt) : t("users.never")}
              </TableCell>
              <TableCell>
                <div className="flex flex-wrap gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => void change(u, { disabled: !u.disabled })}
                  >
                    {t(u.disabled ? "users.enable" : "users.disable")}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setDialog({ kind: "password", user: u })}
                  >
                    {t("users.resetPassword")}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setDialog({ kind: "delete", user: u })}
                  >
                    {t("users.delete")}
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
      setError(message(e, t("users.couldNotCreate")))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent aria-describedby={undefined}>
        <DialogTitle className="text-lg font-semibold">{t("users.addAUser")}</DialogTitle>
        <DialogDescription className="text-sm text-muted-foreground">
          {t("users.thePasswordIsTemporaryThe")}
        </DialogDescription>
        <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-4" noValidate>
          {error ? <Alert variant="destructive">{error}</Alert> : null}
          <div className="flex flex-col gap-2">
            <Label htmlFor="new-username">{t("users.username")}</Label>
            <Input
              id="new-username"
              value={username}
              autoComplete="off"
              onChange={(e) => setUsername(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="new-role">{t("users.role")}</Label>
            <select
              id="new-role"
              className="h-9 rounded-sm border bg-input px-3 text-sm"
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
            <Label htmlFor="new-temp-password">{t("users.temporaryPassword")}</Label>
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
              {t("users.cancel")}
            </Button>
            <Button type="submit" disabled={saving}>
              {t(saving ? "users.saving" : "users.createUser")}
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
      setError(message(e, t("users.couldNotSetPassword")))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent aria-describedby={undefined}>
        <DialogTitle className="text-lg font-semibold">
          {t("users.resetPasswordOf", { name: user.username })}
        </DialogTitle>
        <DialogDescription className="text-sm text-muted-foreground">
          {t("users.theUserIsSignedOut")}
        </DialogDescription>
        <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-4" noValidate>
          {error ? <Alert variant="destructive">{error}</Alert> : null}
          <div className="flex flex-col gap-2">
            <Label htmlFor="reset-password">{t("users.temporaryPassword")}</Label>
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
              {t("users.cancel")}
            </Button>
            <Button type="submit" disabled={saving}>
              {t(saving ? "users.saving" : "users.setPassword")}
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
          {t("users.deleteName", { name: user.username })}
        </AlertDialogTitle>
        <AlertDialogDescription className="text-sm text-muted-foreground">
          {t("users.theAccountItsSessionsAnd")}
        </AlertDialogDescription>
        {error ? (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <AlertDialogCancel>{t("users.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={(e) => {
              e.preventDefault()
              setError("")
              deleteUser(user.id).then(onDone, (err) =>
                setError(message(err, t("users.couldNotDelete"))),
              )
            }}
          >
            {t("users.delete")}
          </AlertDialogAction>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  )
}

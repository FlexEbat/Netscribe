import { useState } from "react"
import { ApiError, downloadBackup, restoreBackup, type BackupSummary } from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

/** What a refused backup or restore tells a person. */
function backupMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 403) return "The password is not right."
    if (error.status === 413) return "The backup is too large."
    if (error.status === 429) return "Too many wrong passwords. Wait a few minutes."
    if ([400, 409, 501].includes(error.status)) return error.message
  }
  return "That did not work. Look at the log of the service."
}

/** Saves the browser's copy of a downloaded file under its name. */
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

/** Backup and restore of the whole install: the database and the config. */
export function BackupCard() {
  const [password, setPassword] = useState("")
  const [file, setFile] = useState<File | null>(null)
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [done, setDone] = useState("")
  const [accepted, setAccepted] = useState<BackupSummary | null>(null)

  function reset() {
    setError("")
    setDone("")
    setAccepted(null)
  }

  async function download() {
    reset()
    setBusy(true)
    try {
      const { blob, filename } = await downloadBackup(password)
      save(blob, filename)
      setDone("The backup was downloaded. It holds the secrets of the config: keep it safe.")
    } catch (e) {
      setError(backupMessage(e))
    } finally {
      setBusy(false)
      setPassword("")
    }
  }

  async function restore() {
    if (!file) return
    reset()
    setBusy(true)
    try {
      setAccepted(await restoreBackup(file, password))
      setFile(null)
    } catch (e) {
      setError(backupMessage(e))
    } finally {
      setBusy(false)
      setConfirming(false)
      setPassword("")
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Backup and restore</CardTitle>
        <CardDescription>
          One file holds the database and the config. The config has its secrets, so the file is as
          private as the config, and both actions ask for your password again.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <Label htmlFor="backup-password">Your password</Label>
          <Input
            id="backup-password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Button disabled={busy || password === ""} onClick={() => void download()}>
            Download a backup
          </Button>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="backup-file">Restore from a backup file</Label>
          <Input
            id="backup-file"
            type="file"
            accept=".gz,.tgz,application/gzip"
            onChange={(e) => {
              setFile(e.target.files?.[0] ?? null)
              setConfirming(false)
            }}
          />
        </div>
        {confirming && file ? (
          <Alert variant="destructive">
            <p>
              This replaces the devices, the accounts and the settings of this install with the ones
              in {file.name}. The old files stay next to the new ones as *.before-restore. The
              service restarts and everybody signs in again.
            </p>
            <div className="mt-3 flex flex-wrap gap-3">
              <Button
                variant="destructive"
                disabled={busy || password === ""}
                onClick={() => void restore()}
              >
                Replace the data
              </Button>
              <Button variant="outline" disabled={busy} onClick={() => setConfirming(false)}>
                Cancel
              </Button>
            </div>
          </Alert>
        ) : (
          <div className="flex flex-wrap items-center gap-3">
            <Button
              variant="outline"
              disabled={busy || !file || password === ""}
              onClick={() => setConfirming(true)}
            >
              Restore
            </Button>
          </div>
        )}
        {error ? <Alert variant="destructive">{error}</Alert> : null}
        {done ? <Alert>{done}</Alert> : null}
        {accepted ? (
          <Alert>
            The backup of {new Date(accepted.createdAt).toLocaleString()} with {accepted.devices}{" "}
            {accepted.devices === 1 ? "device" : "devices"} was accepted. The service restarts now:
            sign in again in a minute.
          </Alert>
        ) : null}
      </CardContent>
    </Card>
  )
}

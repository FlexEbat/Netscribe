import { useCallback, useEffect, useRef, useState } from "react"
import { listAudit, type AuditEntry, type AuditQuery } from "@/api/client"
import { EmptyState } from "@/components/EmptyState"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
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

const pageSize = 50

const actions = [
  "login",
  "login_failed",
  "logout",
  "password_change",
  "user_create",
  "user_update",
  "user_delete",
  "user_password_reset",
  "token_create",
  "token_revoke",
  "scan_start",
  "device_create",
  "device_edit",
  "device_delete",
  "link_create",
  "link_delete",
  "export",
  "config_reload",
  "config_reload_failed",
  "notify_test",
  "access_denied",
]

interface Filters {
  user: string
  action: string
  result: string
  from: string
  to: string
}

const noFilters: Filters = { user: "", action: "", result: "", from: "", to: "" }

type State =
  | { status: "loading" }
  | { status: "failed" }
  | { status: "ready"; entries: AuditEntry[]; more: boolean; loadingMore: boolean }

function toQuery(f: Filters, before?: number): AuditQuery {
  const query: AuditQuery = { limit: pageSize }
  if (f.user.trim()) query.user = f.user.trim()
  if (f.action) query.action = f.action
  if (f.result) query.result = f.result
  if (f.from) query.from = f.from
  if (f.to) query.to = f.to
  if (before !== undefined) query.before = before
  return query
}

export function Audit() {
  const [draft, setDraft] = useState<Filters>(noFilters)
  const [applied, setApplied] = useState<Filters>(noFilters)
  const [state, setState] = useState<State>({ status: "loading" })
  const [moreFailed, setMoreFailed] = useState(false)
  // Counts the filter sets applied so far. A "Load more" answer that arrives after the
  // filters changed belongs to the old filters and must not be appended to the new list.
  const filterRound = useRef(0)

  useEffect(() => {
    filterRound.current += 1
    let cancelled = false
    listAudit(toQuery(applied))
      .then((entries) => {
        if (!cancelled) {
          setState({
            status: "ready",
            entries,
            more: entries.length === pageSize,
            loadingMore: false,
          })
        }
      })
      .catch(() => {
        if (!cancelled) setState({ status: "failed" })
      })
    return () => {
      cancelled = true
    }
  }, [applied])

  const loadMore = useCallback(() => {
    if (state.status !== "ready" || state.loadingMore) return
    const last = state.entries.at(-1)
    if (!last) return
    setMoreFailed(false)
    setState({ ...state, loadingMore: true })
    const round = filterRound.current
    listAudit(toQuery(applied, last.id))
      .then((next) =>
        setState((s) =>
          s.status === "ready" && round === filterRound.current
            ? {
                status: "ready",
                entries: [...s.entries, ...next],
                more: next.length === pageSize,
                loadingMore: false,
              }
            : s,
        ),
      )
      .catch(() => {
        if (round !== filterRound.current) return
        setMoreFailed(true)
        setState((s) => (s.status === "ready" ? { ...s, loadingMore: false } : s))
      })
  }, [state, applied])

  const set = (key: keyof Filters) => (value: string) => setDraft((d) => ({ ...d, [key]: value }))

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-semibold">Audit log</h1>
      <form
        className="flex flex-wrap items-end gap-3"
        onSubmit={(e) => {
          e.preventDefault()
          setState({ status: "loading" })
          setApplied(draft)
        }}
      >
        <div className="flex flex-col gap-2">
          <Label htmlFor="audit-user">User</Label>
          <Input
            id="audit-user"
            className="w-40"
            value={draft.user}
            onChange={(e) => set("user")(e.target.value)}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="audit-action">Action</Label>
          <select
            id="audit-action"
            className="h-9 rounded-lg border bg-input px-3 text-sm"
            value={draft.action}
            onChange={(e) => set("action")(e.target.value)}
          >
            <option value="">Any action</option>
            {actions.map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="audit-result">Result</Label>
          <select
            id="audit-result"
            className="h-9 rounded-lg border bg-input px-3 text-sm"
            value={draft.result}
            onChange={(e) => set("result")(e.target.value)}
          >
            <option value="">Any result</option>
            <option value="ok">ok</option>
            <option value="denied">denied</option>
            <option value="error">error</option>
          </select>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="audit-from">From</Label>
          <Input
            id="audit-from"
            type="date"
            value={draft.from}
            onChange={(e) => set("from")(e.target.value)}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="audit-to">To</Label>
          <Input
            id="audit-to"
            type="date"
            value={draft.to}
            onChange={(e) => set("to")(e.target.value)}
          />
        </div>
        <Button type="submit">Apply</Button>
      </form>

      <Entries state={state} moreFailed={moreFailed} onMore={loadMore} />
    </div>
  )
}

function Entries({
  state,
  moreFailed,
  onMore,
}: {
  state: State
  moreFailed: boolean
  onMore: () => void
}) {
  if (state.status === "loading") {
    return <Skeleton role="status" aria-label="Loading the audit log" className="h-48 w-full" />
  }
  if (state.status === "failed") {
    return <Alert variant="destructive">The audit log could not be loaded. Try again later.</Alert>
  }
  if (state.entries.length === 0) {
    return <EmptyState title="No entries" text="No audit entry matches the filters." />
  }
  return (
    <>
      <Table>
        <TableCaption>Entries shown: {state.entries.length}</TableCaption>
        <TableHeader>
          <TableRow>
            <TableHead>Time</TableHead>
            <TableHead>User</TableHead>
            <TableHead>Action</TableHead>
            <TableHead>Result</TableHead>
            <TableHead>IP</TableHead>
            <TableHead>Detail</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {state.entries.map((e) => (
            <TableRow key={e.id}>
              <TableCell className="whitespace-nowrap">{new Date(e.at).toLocaleString()}</TableCell>
              <TableCell>{e.username || "–"}</TableCell>
              <TableCell className="font-mono text-xs">{e.action}</TableCell>
              <TableCell>{e.result}</TableCell>
              <TableCell className="font-mono text-xs">{e.ip || "–"}</TableCell>
              <TableCell className="break-words">{e.detail}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {moreFailed ? (
        <Alert variant="destructive">More entries could not be loaded. Try again later.</Alert>
      ) : null}
      {state.more ? (
        <div>
          <Button variant="outline" onClick={onMore} disabled={state.loadingMore}>
            {state.loadingMore ? "Loading…" : "Load more"}
          </Button>
        </div>
      ) : null}
    </>
  )
}

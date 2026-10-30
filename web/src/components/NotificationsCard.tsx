import { useEffect, useState } from "react"
import {
  ApiError,
  listNotifications,
  testNotification,
  type NotificationChannel,
} from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

type State =
  | { status: "loading" }
  | { status: "failed" }
  | { status: "ready"; channels: NotificationChannel[] }

/** What the answer to "Send test" tells a person. */
function testMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 429) return "A test message was sent a moment ago. Wait a few seconds."
    if (error.status === 404) return "This channel is not in the config any more."
    if (error.status === 502) return error.message
  }
  return "The test message could not be sent."
}

/** The channels of the config, read only: they hold secrets, so they are edited in the file. */
export function NotificationsCard() {
  const [state, setState] = useState<State>({ status: "loading" })
  const [results, setResults] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState("")

  useEffect(() => {
    let cancelled = false
    listNotifications()
      .then((channels) => !cancelled && setState({ status: "ready", channels }))
      .catch(() => !cancelled && setState({ status: "failed" }))
    return () => {
      cancelled = true
    }
  }, [])

  async function send(name: string) {
    setBusy(name)
    try {
      await testNotification(name)
      setResults((r) => ({ ...r, [name]: "The test message was sent." }))
    } catch (error) {
      setResults((r) => ({ ...r, [name]: testMessage(error) }))
    } finally {
      setBusy("")
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Notifications</CardTitle>
        <CardDescription>
          Messages about new devices, devices that went offline, new open ports and failed scans.
          Channels hold addresses and tokens, so they are set under{" "}
          <code className="font-mono">notify</code> in the config file.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {state.status === "loading" ? (
          <p role="status" className="text-sm text-muted-foreground">
            Loading the channels
          </p>
        ) : null}
        {state.status === "failed" ? (
          <Alert variant="destructive">The channels could not be loaded.</Alert>
        ) : null}
        {state.status === "ready" && state.channels.length === 0 ? (
          <p className="text-sm text-muted-foreground">No channel is configured.</p>
        ) : null}
        {state.status === "ready"
          ? state.channels.map((c) => (
              <div key={c.name} className="flex flex-wrap items-center gap-3">
                <span className="font-medium">{c.name}</span>
                <Badge variant="outline">{c.type}</Badge>
                <span className="text-sm text-muted-foreground">
                  {c.events.length > 0 ? c.events.join(", ") : "no events selected"}
                </span>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={busy !== ""}
                  aria-label={`Send a test message to ${c.name}`}
                  onClick={() => void send(c.name)}
                >
                  Send test
                </Button>
                {results[c.name] ? (
                  <span role="status" className="text-sm text-muted-foreground">
                    {results[c.name]}
                  </span>
                ) : null}
              </div>
            ))
          : null}
      </CardContent>
    </Card>
  )
}

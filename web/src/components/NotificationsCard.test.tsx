import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import { NotificationsCard } from "@/components/NotificationsCard"
import { json, mockFetch } from "@/test-utils"

afterEach(() => vi.unstubAllGlobals())

const channels = [
  { name: "ops", type: "webhook", events: ["device_added", "scan_failed"] },
  { name: "phone", type: "telegram", events: [] },
]

function open(test: () => Response = () => json(200, { status: "sent" })) {
  const mock = mockFetch({
    "GET /api/notifications": () => json(200, { channels }),
    "POST /api/notifications/test": test,
  })
  render(<NotificationsCard />)
  return mock
}

describe("NotificationsCard", () => {
  it("lists the channels with their type and events", async () => {
    open()
    expect(await screen.findByText("ops")).toBeInTheDocument()
    expect(screen.getByText("webhook")).toBeInTheDocument()
    expect(screen.getByText("device_added, scan_failed")).toBeInTheDocument()
    expect(screen.getByText("no events selected")).toBeInTheDocument()
  })

  it("says so when no channel is configured", async () => {
    mockFetch({ "GET /api/notifications": () => json(200, { channels: [] }) })
    render(<NotificationsCard />)
    expect(await screen.findByText("No channel is configured.")).toBeInTheDocument()
  })

  it("reports a failed load", async () => {
    mockFetch({})
    render(<NotificationsCard />)
    expect(await screen.findByRole("alert")).toHaveTextContent("The channels could not be loaded.")
  })

  it("sends a test message to the chosen channel only", async () => {
    const mock = open()
    await userEvent
      .setup()
      .click(await screen.findByRole("button", { name: /test message to phone/ }))
    expect(await screen.findByText("The test message was sent.")).toBeInTheDocument()
    const posts = mock.calls.filter((c) => c.method === "POST")
    expect(posts).toHaveLength(1)
    expect(JSON.parse(String(posts[0]!.init.body))).toEqual({ channel: "phone" })
  })

  it("explains a second test within seconds", async () => {
    open(() =>
      json(429, { error: "a test message was sent a moment ago" }, { "Retry-After": "10" }),
    )
    await userEvent
      .setup()
      .click(await screen.findByRole("button", { name: /test message to ops/ }))
    expect(await screen.findByText(/Wait a few seconds/)).toBeInTheDocument()
  })

  it("shows why a delivery failed, and nothing else from the server", async () => {
    open(() => json(502, { error: "the message was not delivered: timeout" }))
    await userEvent
      .setup()
      .click(await screen.findByRole("button", { name: /test message to ops/ }))
    await waitFor(() =>
      expect(screen.getByText("the message was not delivered: timeout")).toBeInTheDocument(),
    )
  })
})

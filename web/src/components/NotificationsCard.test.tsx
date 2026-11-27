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

const rules = {
  flapCount: 3,
  quietHours: "22:00-07:00",
  maintenance: [
    { from: "2099-12-01T22:00", to: "2099-12-02T02:00", group: "Lab" },
    { from: "2020-01-01T00:00", to: "2020-01-01T02:00", group: "" },
  ],
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

  it("shows the steady alert rules read only", async () => {
    mockFetch({ "GET /api/notifications": () => json(200, { channels, rules }) })
    render(<NotificationsCard />)
    expect(await screen.findByText("Steady alerts")).toBeInTheDocument()
    expect(screen.getByText(/changes state 3 times within an hour/)).toBeInTheDocument()
    expect(screen.getByText(/Quiet hours 22:00-07:00/)).toBeInTheDocument()
    expect(
      screen.getByText(/Maintenance 2099-12-01 22:00 to 2099-12-02 02:00: Lab/),
    ).toBeInTheDocument()
    expect(screen.getByText(/the whole network \(over\)/)).toBeInTheDocument()
    expect(screen.getByText("Edit these in the config file.")).toBeInTheDocument()
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument()
  })

  it("says there are no quiet hours and no windows", async () => {
    mockFetch({
      "GET /api/notifications": () =>
        json(200, { channels, rules: { flapCount: 3, quietHours: "", maintenance: [] } }),
    })
    render(<NotificationsCard />)
    expect(await screen.findByText("No quiet hours.")).toBeInTheDocument()
    expect(screen.getByText("No maintenance window.")).toBeInTheDocument()
  })

  it("shows no rules when the server does not send them", async () => {
    open()
    await screen.findByText("ops")
    expect(screen.queryByText("Steady alerts")).not.toBeInTheDocument()
  })
})

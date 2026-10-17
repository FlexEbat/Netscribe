import { act, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { Change, Scan } from "@/api/client"
import { Changes } from "@/pages/Changes"
import { json, mockFetch } from "@/test-utils"

afterEach(() => vi.unstubAllGlobals())

class FakeEventSource {
  static last: FakeEventSource | null = null
  private listeners = new Map<string, (e: MessageEvent<string>) => void>()
  constructor() {
    FakeEventSource.last = this
  }
  addEventListener(name: string, fn: (e: MessageEvent<string>) => void) {
    this.listeners.set(name, fn)
  }
  close() {}
  emit(name: string, data: unknown = {}) {
    this.listeners.get(name)?.(new MessageEvent(name, { data: JSON.stringify(data) }))
  }
}

function scan(id: number, status: Scan["status"], devices: number): Scan {
  return {
    id,
    target: "192.168.1.0/24",
    status,
    startedAt: "2026-10-09T12:00:00.000Z",
    finishedAt: status === "running" ? null : "2026-10-09T12:01:00.000Z",
    error: "",
    deviceCount: devices,
    linkCount: 0,
    containerCount: 0,
  }
}

function change(id: number, kind: Change["kind"], summary: string): Change {
  return { id, scanId: 2, kind, entityId: String(id), summary, at: "2026-10-09T12:01:00.000Z" }
}

function setup(state: () => { changes: Change[]; scans: Scan[] }) {
  vi.stubGlobal("EventSource", FakeEventSource)
  const mock = mockFetch({
    "GET /api/changes?limit=200": () => json(200, { changes: state().changes }),
    "GET /api/scans": () => json(200, { scans: state().scans }),
  })
  render(<Changes />)
  return mock
}

describe("Changes page", () => {
  it("shows the feed, newest first as the server sent it, and the chart", async () => {
    setup(() => ({
      changes: [
        change(2, "device_offline", "web went offline"),
        change(1, "device_added", "New device 192.168.1.9 (-, printer)"),
      ],
      scans: [scan(3, "done", 7), scan(2, "done", 8), scan(1, "done", 5)],
    }))
    const feed = await screen.findByRole("list", { name: "Changes" })
    const items = within(feed).getAllByRole("listitem")
    expect(items).toHaveLength(2)
    expect(within(items[0]!).getByText("Device offline")).toBeInTheDocument()
    expect(within(items[0]!).getByText("web went offline")).toBeInTheDocument()
    expect(within(items[1]!).getByText("Device added")).toBeInTheDocument()
    expect(within(items[1]!).getByText(/192\.168\.1\.9/)).toBeInTheDocument()

    const chart = screen.getByRole("figure", { name: "Devices per scan" })
    expect(within(chart).getByText(/Last: 7 in scan #3/)).toBeInTheDocument()
    expect(chart.querySelector(".recharts-responsive-container")).not.toBeNull()
  })

  it("shows the text of a summary, not markup", async () => {
    setup(() => ({
      changes: [change(1, "device_added", "<img src=x onerror=alert(1)>")],
      scans: [scan(1, "done", 1)],
    }))
    const feed = await screen.findByRole("list", { name: "Changes" })
    expect(within(feed).getByText("<img src=x onerror=alert(1)>")).toBeInTheDocument()
    expect(feed.querySelector("img")).toBeNull()
  })

  it("shows an empty state when there are no changes", async () => {
    setup(() => ({ changes: [], scans: [scan(1, "done", 3)] }))
    expect(await screen.findByText("No changes yet")).toBeInTheDocument()
    expect(screen.queryByRole("list", { name: "Changes" })).not.toBeInTheDocument()
    // The chart still shows the one completed scan.
    expect(screen.getByRole("figure", { name: "Devices per scan" })).toBeInTheDocument()
  })

  it("keeps failed and running scans off the chart", async () => {
    setup(() => ({
      changes: [],
      scans: [scan(3, "running", 0), scan(2, "failed", 0), scan(1, "done", 4)],
    }))
    expect(await screen.findByText(/Last: 4 in scan #1/)).toBeInTheDocument()
  })

  it("says what is missing before the first scan", async () => {
    setup(() => ({ changes: [], scans: [] }))
    expect(await screen.findByText("No changes yet")).toBeInTheDocument()
    expect(screen.getByText(/appears after the first completed scan/)).toBeInTheDocument()
    expect(screen.queryByRole("figure")).not.toBeInTheDocument()
  })

  it("shows an error when the feed cannot be loaded", async () => {
    vi.stubGlobal("EventSource", FakeEventSource)
    mockFetch({
      "GET /api/changes?limit=200": () => json(500, { error: "boom" }),
      "GET /api/scans": () => json(200, { scans: [] }),
    })
    render(<Changes />)
    expect(await screen.findByText(/could not be loaded/)).toBeInTheDocument()
  })

  it("updates when a scan finishes", async () => {
    let current = { changes: [] as Change[], scans: [scan(1, "done", 3)] }
    const mock = setup(() => current)
    await screen.findByText("No changes yet")

    current = {
      changes: [change(1, "link_added", "Link appeared: gw Gi0/1 - web eth0")],
      scans: [scan(2, "done", 3), scan(1, "done", 3)],
    }
    act(() => FakeEventSource.last?.emit("scan.finished", scan(2, "done", 3)))
    expect(await screen.findByText("Link added")).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByText("No changes yet")).not.toBeInTheDocument())
    expect(mock.calls.filter((c) => c.path.startsWith("/api/changes")).length).toBe(2)
  })
})

import { act, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { Overview as OverviewData } from "@/api/client"
import { Overview } from "@/pages/Overview"
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
  emit(name: string) {
    this.listeners.get(name)?.(new MessageEvent(name, { data: "{}" }))
  }
}

const scan = {
  id: 7,
  target: "192.168.1.0/24",
  status: "done" as const,
  startedAt: "2026-10-09T12:00:00.000Z",
  finishedAt: "2026-10-09T12:01:00.000Z",
  error: "",
  deviceCount: 12,
  linkCount: 0,
  containerCount: 0,
}

const data: OverviewData = {
  devices: { total: 12, online: 9, offline: 3, unknown: 2, newThisWeek: 1 },
  risky: 2,
  lastScan: scan,
  recentScans: { total: 10, failed: 1 },
  changes: [
    {
      id: 1,
      scanId: 7,
      kind: "device_added",
      entityId: "x",
      summary: "New device cam1",
      at: "2026-10-09T12:01:00.000Z",
    },
  ],
}

function open(overview: () => Response) {
  vi.stubGlobal("EventSource", FakeEventSource)
  const mock = mockFetch({ "GET /api/overview": overview })
  render(
    <MemoryRouter>
      <Overview />
    </MemoryRouter>,
  )
  return mock
}

describe("Overview", () => {
  it("shows the counts and leads each tile to the page with the details", async () => {
    open(() => json(200, { overview: data }))
    const tile = async (label: string) => (await screen.findByText(label)).closest("a")!
    expect(within(await tile("Online")).getByText("9")).toBeInTheDocument()
    expect(await tile("Online")).toHaveAttribute("href", "/inventory?status=online")
    expect(await tile("Offline")).toHaveAttribute("href", "/inventory?status=offline")
    expect(await tile("Unknown")).toHaveAttribute("href", "/inventory?trust=unknown")
    expect(await tile("Devices")).toHaveAttribute("href", "/inventory")
    expect(await tile("Risky services")).toHaveAttribute("href", "/services")
    expect(within(await tile("New this week")).getByText("1")).toBeInTheDocument()
  })

  it("says how healthy the scans are and lists the latest changes", async () => {
    open(() => json(200, { overview: data }))
    expect(await screen.findByText("1 of the last 10 scans failed.")).toBeInTheDocument()
    expect(screen.getByText("New device cam1")).toBeInTheDocument()
    expect(screen.getByText("Device added")).toBeInTheDocument()
  })

  it("says that every scan finished when none failed", async () => {
    open(() => json(200, { overview: { ...data, recentScans: { total: 4, failed: 0 } } }))
    expect(await screen.findByText("All of the last 4 scans finished.")).toBeInTheDocument()
  })

  it("shows an empty state before the first scan, and no numbers", async () => {
    open(() =>
      json(200, {
        overview: {
          devices: { total: 0, online: 0, offline: 0, unknown: 0, newThisWeek: 0 },
          risky: 0,
          lastScan: null,
          recentScans: { total: 0, failed: 0 },
          changes: [],
        },
      }),
    )
    expect(await screen.findByText("Nothing to show yet")).toBeInTheDocument()
    expect(screen.queryByText("Online")).not.toBeInTheDocument()
  })

  it("reports a failed load", async () => {
    open(() => json(500, { error: "internal error" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("The overview could not be loaded")
  })

  it("loads again when a scan finishes", async () => {
    const mock = open(() => json(200, { overview: data }))
    await screen.findByText("Online")
    act(() => FakeEventSource.last!.emit("scan.finished"))
    await waitFor(() =>
      expect(mock.calls.filter((c) => c.path === "/api/overview")).toHaveLength(2),
    )
  })
})

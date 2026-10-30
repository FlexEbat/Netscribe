import { render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { DeviceHistory } from "@/api/client"
import { HistoryTab } from "@/components/HistoryTab"
import { json, mockFetch } from "@/test-utils"

afterEach(() => vi.unstubAllGlobals())

const history: DeviceHistory = {
  scans: 4,
  online: 3,
  uptime: 75,
  ips: [
    { ip: "192.168.1.77", from: "2026-10-09T12:20:00Z", to: "2026-10-09T12:30:00Z" },
    { ip: "192.168.1.5", from: "2026-10-09T12:00:00Z", to: "2026-10-09T12:00:00Z" },
  ],
  outages: [{ from: "2026-10-09T12:30:00Z", to: null }],
  points: [],
}

function open(h: DeviceHistory | null, status = 200) {
  mockFetch({
    "GET /api/devices/3/history": () =>
      h ? json(status, { history: h }) : json(404, { error: "not found" }),
  })
  render(<HistoryTab deviceId={3} />)
}

describe("HistoryTab", () => {
  it("shows the uptime with the scans it rests on, the addresses and the outage that is going on", async () => {
    open(history)
    expect(await screen.findByText("75%")).toBeInTheDocument()
    expect(screen.getByText(/seen in 3 of 4 stored scans/)).toBeInTheDocument()
    expect(screen.getByText("192.168.1.77")).toBeInTheDocument()
    expect(screen.getByText("192.168.1.5")).toBeInTheDocument()
    expect(screen.getByText("still away")).toBeInTheDocument()
  })

  it("says so when no scan has missed the device", async () => {
    open({ ...history, outages: [] })
    expect(await screen.findByText("No scan has missed this device.")).toBeInTheDocument()
  })

  it("explains an empty history instead of showing 0%", async () => {
    open({ ...history, scans: 0, online: 0, uptime: null, ips: [], outages: [] })
    expect(await screen.findByText(/No scan has seen this device/)).toBeInTheDocument()
    expect(screen.queryByText(/%/)).not.toBeInTheDocument()
  })

  it("reports a failed load", async () => {
    open(null)
    expect(await screen.findByRole("alert")).toHaveTextContent("The history could not be loaded.")
  })
})

import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { Device, DeviceInterface } from "@/api/client"
import { DevicePanel } from "@/components/DevicePanel"
import { json, mockFetch } from "@/test-utils"

afterEach(() => vi.unstubAllGlobals())

const device: Device = {
  id: 3,
  mac: "aa:bb:cc:dd:ee:03",
  ip: "192.168.1.3",
  hostname: "sw1",
  vendor: "Cisco Systems",
  kind: "switch",
  description: "Acme Switch 24G",
  source: "snmp",
  online: true,
  manual: false,
  label: "",
  notes: "",
  kindLocked: false,
  version: 1,
  firstSeenAt: "2026-10-09T12:00:00.000Z",
  lastSeenAt: "2026-10-09T12:00:00.000Z",
  x: null,
  y: null,
}

function iface(id: number, overrides: Partial<DeviceInterface> = {}): DeviceInterface {
  return { id, deviceId: 3, name: `Gi${id}`, mac: "", ip: "", up: true, ...overrides }
}

function open(body: unknown) {
  mockFetch({ "GET /api/devices/3": () => json(200, body) })
  render(<DevicePanel deviceId={3} onClose={() => {}} />)
}

describe("DevicePanel", () => {
  it("shows the vendor and the source on the details tab", async () => {
    open({ device, interfaces: [] })
    expect(await screen.findByText("Cisco Systems")).toBeInTheDocument()
    expect(screen.getByText("snmp")).toBeInTheDocument()
  })

  it("lists the interfaces with name, MAC, IP and state", async () => {
    open({
      device,
      interfaces: [
        iface(1, { name: "mgmt0", mac: "02:11:22:33:44:01", ip: "192.168.1.3" }),
        iface(2, { name: "Gi2", up: false }),
      ],
    })
    const user = userEvent.setup()
    await user.click(await screen.findByRole("tab", { name: "Interfaces (2)" }))

    const rows = within(screen.getByRole("table")).getAllByRole("row")
    expect(rows).toHaveLength(3) // the header and two interfaces
    expect(within(rows[1]!).getByText("mgmt0")).toBeInTheDocument()
    expect(within(rows[1]!).getByText("02:11:22:33:44:01")).toBeInTheDocument()
    expect(within(rows[1]!).getByText("192.168.1.3")).toBeInTheDocument()
    expect(within(rows[1]!).getByText("Up")).toBeInTheDocument()
    expect(within(rows[2]!).getByText("Down")).toBeInTheDocument()
  })

  it("says so when the device has no interfaces", async () => {
    open({ device, interfaces: [] })
    const user = userEvent.setup()
    await user.click(await screen.findByRole("tab", { name: "Interfaces (0)" }))
    expect(screen.getByText("No interfaces were read from this device.")).toBeInTheDocument()
  })

  it("copes with an answer that has no interfaces field", async () => {
    open({ device })
    expect(await screen.findByRole("tab", { name: "Interfaces (0)" })).toBeInTheDocument()
  })
})

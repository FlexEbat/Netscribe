import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { Device, Service } from "@/api/client"
import { Services } from "@/pages/Services"
import { json, mockFetch } from "@/test-utils"

afterEach(() => vi.unstubAllGlobals())

function device(id: number, ip: string, hostname: string): Device {
  return {
    id,
    mac: `aa:bb:cc:dd:ee:0${id}`,
    ip,
    hostname,
    vendor: "",
    kind: "host",
    description: "",
    source: "arp",
    online: true,
    manual: false,
    label: "",
    notes: "",
    osGuess: "",
    kindLocked: false,
    trusted: true,
    version: 1,
    firstSeenAt: "2026-10-09T12:00:00.000Z",
    lastSeenAt: "2026-10-09T12:00:00.000Z",
    x: null,
    y: null,
  }
}

function service(id: number, deviceId: number, port: number, name: string): Service {
  return {
    id,
    deviceId,
    port,
    proto: "tcp",
    name,
    banner: "",
    firstSeenAt: "2026-10-09T12:00:00.000Z",
    lastSeenAt: "2026-10-09T12:00:00.000Z",
    risk: null,
  }
}

function setup(services: Service[], devices: Device[]) {
  mockFetch({
    "GET /api/services": () => json(200, { services }),
    "GET /api/devices": () => json(200, { devices }),
  })
  render(<Services />)
}

describe("Services page", () => {
  it("shows the name, port, protocol and device of every service", async () => {
    setup(
      [service(1, 2, 443, "https"), service(2, 1, 22, "ssh")],
      [device(1, "192.168.1.1", "gateway"), device(2, "192.168.1.2", "")],
    )
    const rows = within(await screen.findByRole("table")).getAllByRole("row")
    expect(rows).toHaveLength(3)
    // Ordered by the IP of the device.
    expect(within(rows[1]!).getByText("ssh")).toBeInTheDocument()
    expect(within(rows[1]!).getByText("22")).toBeInTheDocument()
    expect(within(rows[1]!).getByText("tcp")).toBeInTheDocument()
    expect(within(rows[1]!).getByText("gateway")).toBeInTheDocument()
    expect(within(rows[2]!).getByText("https")).toBeInTheDocument()
    expect(within(rows[2]!).getByText("192.168.1.2")).toBeInTheDocument()
  })

  it("filters by the name of the service", async () => {
    setup(
      [service(1, 1, 22, "ssh"), service(2, 1, 443, "https"), service(3, 1, 8080, "http-alt")],
      [device(1, "192.168.1.1", "gateway")],
    )
    const user = userEvent.setup()
    await screen.findByRole("table")
    await user.type(screen.getByLabelText("Filter by service name or banner"), "http")
    const rows = within(screen.getByRole("table")).getAllByRole("row")
    expect(rows).toHaveLength(3) // the header, https and http-alt
    expect(screen.queryByText("ssh")).not.toBeInTheDocument()
  })

  it("says so when the filter matches nothing", async () => {
    setup([service(1, 1, 22, "ssh")], [device(1, "192.168.1.1", "gateway")])
    const user = userEvent.setup()
    await screen.findByRole("table")
    await user.type(screen.getByLabelText("Filter by service name or banner"), "smtp")
    expect(screen.getByText("No service matches the filter.")).toBeInTheDocument()
  })

  it("explains an empty list", async () => {
    setup([], [])
    expect(await screen.findByText("No services yet")).toBeInTheDocument()
  })

  it("reports a failure", async () => {
    mockFetch({
      "GET /api/services": () => json(500, { error: "boom" }),
      "GET /api/devices": () => json(200, { devices: [] }),
    })
    render(<Services />)
    expect(await screen.findByText(/could not be loaded/)).toBeInTheDocument()
  })
})

describe("Services banner", () => {
  it("shows the banner of a service and filters by it", async () => {
    const withBanner = { ...service(1, 1, 80, "http"), banner: "nginx/1.25.3" }
    setup([withBanner, service(2, 1, 22, "ssh")], [device(1, "192.168.1.1", "gateway")])
    expect(await screen.findByText("nginx/1.25.3")).toBeInTheDocument()

    await userEvent.type(screen.getByLabelText("Filter by service name or banner"), "nginx")
    expect(screen.getByText("nginx/1.25.3")).toBeInTheDocument()
    expect(screen.queryByText("ssh")).not.toBeInTheDocument()
  })

  it("marks a risky service and leaves the others alone", async () => {
    const telnet = {
      ...service(1, 1, 23, "telnet"),
      risk: { severity: "high" as const, message: "Telnet sends passwords in clear text" },
    }
    setup([telnet, service(2, 1, 22, "ssh")], [device(1, "192.168.1.1", "gateway")])
    const rows = within(await screen.findByRole("table")).getAllByRole("row")
    const telnetRow = rows.find((r) => within(r).queryByText("telnet"))!
    const sshRow = rows.find((r) => within(r).queryByText("ssh"))!
    expect(within(telnetRow).getByText("high risk")).toHaveAttribute(
      "title",
      "Telnet sends passwords in clear text",
    )
    expect(within(sshRow).queryByText(/risk/)).not.toBeInTheDocument()
  })
})

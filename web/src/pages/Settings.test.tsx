import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import { setCsrfToken, type SettingsGroup, type VersionInfo } from "@/api/client"
import { Settings } from "@/pages/Settings"
import { json, mockFetch } from "@/test-utils"

afterEach(() => {
  setCsrfToken("")
  vi.unstubAllGlobals()
})

const groups: SettingsGroup[] = [
  {
    name: "Scanning",
    hint: "what is scanned",
    settings: [
      {
        key: "scan.interval",
        label: "Scan every",
        help: "",
        type: "duration",
        value: "15m",
        default: "15m",
        hot: true,
      },
      {
        key: "scan.ports",
        label: "TCP ports",
        help: "",
        type: "ports",
        value: ["22", "80"],
        default: "22",
        hot: true,
      },
    ],
  },
  {
    name: "Discovery",
    hint: "how devices are told apart",
    settings: [
      {
        key: "discovery.mdns",
        label: "mDNS",
        help: "",
        type: "bool",
        value: true,
        default: "on",
        hot: false,
      },
      {
        key: "discovery.nmapScope",
        label: "Nmap scope",
        help: "",
        type: "enum",
        value: "untyped",
        default: "untyped",
        choices: ["untyped", "all"],
        hot: false,
      },
    ],
  },
  {
    name: "SNMP",
    hint: "device queries",
    settings: [
      {
        key: "snmp.communities",
        label: "Communities",
        help: "",
        type: "secrets",
        value: null,
        default: "",
        hot: true,
        count: 2,
      },
    ],
  },
]

const version: VersionInfo = {
  version: "v0.2.0",
  update: {
    current: "v0.2.0",
    latest: null,
    available: false,
    checking: true,
    checkedAt: "",
    releasesUrl: "https://github.com/FlexEbat/Netscribe/releases",
    changelogUrl: "https://github.com/FlexEbat/Netscribe/releases",
  },
  helper: { available: true, state: "idle", action: "", message: "", at: null },
}

function open(extra: Record<string, () => Response> = {}) {
  return mockFetch({
    "GET /api/settings": () => json(200, { groups }),
    "GET /api/version": () => json(200, version),
    ...extra,
  })
}

function sent(mock: ReturnType<typeof open>): Record<string, unknown> {
  const call = mock.calls.find((c) => c.method === "PUT" && c.path === "/api/settings")
  return (JSON.parse(String(call?.init.body)) as { changes: Record<string, unknown> }).changes
}

describe("Settings", () => {
  it("lists every group with the values in use", async () => {
    open()
    render(<Settings />)
    expect(await screen.findByDisplayValue("15m")).toBeTruthy()
    expect(screen.getByDisplayValue("22, 80")).toBeTruthy()
    for (const name of ["Scanning", "Discovery", "SNMP"])
      expect(screen.getByText(name)).toBeTruthy()
    expect(screen.getByLabelText("Communities").getAttribute("placeholder")).toContain("2 set")
  })

  it("sends only what was changed, in the shape the server expects", async () => {
    const mock = open({
      "PUT /api/settings": () =>
        json(200, { saved: ["scan.interval", "scan.ports"], needRestart: [] }),
    })
    render(<Settings />)
    const interval = await screen.findByLabelText("Scan every")
    await userEvent.clear(interval)
    await userEvent.type(interval, "30m")
    const ports = screen.getByLabelText("TCP ports")
    await userEvent.clear(ports)
    await userEvent.type(ports, "22, 443 8080")
    await userEvent.click(screen.getByRole("button", { name: "Save changes" }))
    await screen.findByText(/Saved 2 settings/)
    expect(sent(mock)).toEqual({ "scan.interval": "30m", "scan.ports": ["22", "443", "8080"] })
  })

  it("keeps Save disabled until something changes", async () => {
    open()
    render(<Settings />)
    const save = await screen.findByRole("button", { name: "Save changes" })
    expect((save as HTMLButtonElement).disabled).toBe(true)
    await userEvent.click(screen.getByLabelText("mDNS"))
    expect((save as HTMLButtonElement).disabled).toBe(false)
    await userEvent.click(screen.getByRole("button", { name: "Discard" }))
    expect((save as HTMLButtonElement).disabled).toBe(true)
  })

  it("offers a restart for a setting that needs one", async () => {
    const mock = open({
      "PUT /api/settings": () =>
        json(200, { saved: ["discovery.mdns"], needRestart: ["discovery.mdns"] }),
      "POST /api/system/restart": () => json(202, version),
    })
    render(<Settings />)
    await userEvent.click(await screen.findByLabelText("mDNS"))
    await userEvent.click(screen.getByRole("button", { name: "Save changes" }))
    expect(sent(mock)).toEqual({ "discovery.mdns": false })
    await userEvent.click(await screen.findByRole("button", { name: "Restart now" }))
    await screen.findByText(/Restarting/)
    expect(mock.calls.some((c) => c.method === "POST" && c.path === "/api/system/restart")).toBe(
      true,
    )
  })

  it("sends new secrets only when they were typed, and never shows old ones", async () => {
    const mock = open({
      "PUT /api/settings": () => json(200, { saved: ["snmp.communities"], needRestart: [] }),
    })
    render(<Settings />)
    const field = await screen.findByLabelText("Communities")
    expect((field as HTMLInputElement).type).toBe("password")
    expect((field as HTMLInputElement).value).toBe("")
    await userEvent.type(field, "alpha, beta")
    await userEvent.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(sent(mock)).toEqual({ "snmp.communities": ["alpha", "beta"] }))
  })

  it("shows the reason when the server refuses a value", async () => {
    open({
      "PUT /api/settings": () => json(400, { error: "Scan every: must be a duration such as 15m" }),
    })
    render(<Settings />)
    const interval = await screen.findByLabelText("Scan every")
    await userEvent.clear(interval)
    await userEvent.type(interval, "soon")
    await userEvent.click(screen.getByRole("button", { name: "Save changes" }))
    expect(await screen.findByText(/must be a duration/)).toBeTruthy()
  })

  it("checks for updates on request", async () => {
    open({ "POST /api/update/check": () => json(200, version) })
    render(<Settings />)
    await userEvent.click(await screen.findByRole("button", { name: "Check for updates" }))
    expect(await screen.findByText("This is the newest release.")).toBeTruthy()
  })
})

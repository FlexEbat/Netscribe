import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import { setCsrfToken, type VersionInfo } from "@/api/client"
import { VersionBadge } from "@/components/VersionBadge"
import type { Permission } from "@/lib/permissions"
import { json, mockFetch } from "@/test-utils"

afterEach(() => {
  setCsrfToken("")
  vi.unstubAllGlobals()
})

function info(over: Partial<VersionInfo> = {}, available = false): VersionInfo {
  return {
    version: "v0.2.0",
    update: {
      current: "v0.2.0",
      latest: available
        ? {
            tag: "v0.3.0",
            version: "0.3.0",
            url: "https://github.com/FlexEbat/Netscribe/releases/tag/v0.3.0",
            notes: "- faster scans",
            publishedAt: "2026-10-20T10:00:00Z",
          }
        : null,
      available,
      checking: true,
      checkedAt: "2026-10-28T10:00:00Z",
      releasesUrl: "https://github.com/FlexEbat/Netscribe/releases",
      changelogUrl: "https://github.com/FlexEbat/Netscribe/releases/tag/v0.3.0",
    },
    helper: { available: true, state: "idle", action: "", message: "", at: null },
    ...over,
  }
}

const admin: Permission[] = ["topology:read", "system:manage"]

describe("VersionBadge", () => {
  it("shows the running version without a glow when it is current", async () => {
    mockFetch({ "GET /api/version": () => json(200, info()) })
    render(<VersionBadge permissions={admin} />)
    const badge = await screen.findByText("v0.2.0")
    expect(badge.className).not.toContain("update-glow")
    expect(screen.queryByRole("button")).toBeNull()
  })

  it("glows and opens the question when a newer release exists", async () => {
    mockFetch({ "GET /api/version": () => json(200, info({}, true)) })
    render(<VersionBadge permissions={admin} />)
    const button = await screen.findByRole("button", { name: /v0\.3\.0 is available/ })
    expect(button.className).toContain("update-glow")

    await userEvent.click(button)
    expect(await screen.findByText("A new version is available")).toBeTruthy()
    expect(screen.getByDisplayValue("- faster scans")).toBeTruthy()
    const links = screen.getAllByRole("link").map((a) => a.getAttribute("href"))
    expect(links).toContain("https://github.com/FlexEbat/Netscribe/releases/tag/v0.3.0")
    expect(links).toContain("https://github.com/FlexEbat/Netscribe/releases")
    expect(screen.getByRole("button", { name: "Not now" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Update now" })).toBeTruthy()
  })

  it("does nothing when the answer is Not now", async () => {
    const mock = mockFetch({ "GET /api/version": () => json(200, info({}, true)) })
    render(<VersionBadge permissions={admin} />)
    await userEvent.click(await screen.findByRole("button", { name: /is available/ }))
    await userEvent.click(await screen.findByRole("button", { name: "Not now" }))
    await waitFor(() => expect(screen.queryByText("A new version is available")).toBeNull())
    expect(mock.calls.some((c) => c.method === "POST")).toBe(false)
  })

  it("asks the server to update only after Update now", async () => {
    const mock = mockFetch({
      "GET /api/version": () => json(200, info({}, true)),
      "POST /api/update": () => json(202, info({}, true)),
    })
    render(<VersionBadge permissions={admin} />)
    await userEvent.click(await screen.findByRole("button", { name: /is available/ }))
    await userEvent.click(await screen.findByRole("button", { name: "Update now" }))
    await screen.findByText(/Updating to v0\.3\.0/)
    expect(mock.calls.filter((c) => c.method === "POST" && c.path === "/api/update")).toHaveLength(
      1,
    )
  })

  it("offers no update button to a role that may not update", async () => {
    mockFetch({ "GET /api/version": () => json(200, info({}, true)) })
    render(<VersionBadge permissions={["topology:read"]} />)
    await userEvent.click(await screen.findByRole("button", { name: /is available/ }))
    expect(await screen.findByText(/Only an administrator can update/)).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Update now" })).toBeNull()
  })

  it("shows the command when the install has no helper", async () => {
    const noHelper = info(
      { helper: { available: false, state: "idle", action: "", message: "", at: null } },
      true,
    )
    mockFetch({ "GET /api/version": () => json(200, noHelper) })
    render(<VersionBadge permissions={admin} />)
    await userEvent.click(await screen.findByRole("button", { name: /is available/ }))
    expect(await screen.findByText(/cannot update itself/)).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Update now" })).toBeNull()
  })

  it("does not make a link out of an address outside the project", async () => {
    const bad = info({}, true)
    if (bad.update.latest) bad.update.latest.url = "https://evil.example/x"
    bad.update.changelogUrl = "https://evil.example/y"
    bad.update.releasesUrl = "https://evil.example/z"
    mockFetch({ "GET /api/version": () => json(200, bad) })
    render(<VersionBadge permissions={admin} />)
    await userEvent.click(await screen.findByRole("button", { name: /is available/ }))
    await screen.findByText("A new version is available")
    expect(screen.queryAllByRole("link")).toHaveLength(0)
  })

  it("shows nothing when the version cannot be read", async () => {
    mockFetch({})
    const { container } = render(<VersionBadge permissions={admin} />)
    await new Promise((r) => setTimeout(r, 30))
    expect(container.textContent).toBe("")
  })
})

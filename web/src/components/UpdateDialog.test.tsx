import { act, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import { setCsrfToken, type VersionInfo } from "@/api/client"
import { UpdateDialog } from "@/components/UpdateDialog"
import { json, mockFetch } from "@/test-utils"

afterEach(() => {
  setCsrfToken("")
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

function info(version: string, helper: Partial<VersionInfo["helper"]> = {}): VersionInfo {
  return {
    version,
    update: {
      current: version,
      latest: {
        tag: "v0.3.0",
        version: "0.3.0",
        url: "https://github.com/FlexEbat/Netscribe/releases/tag/v0.3.0",
        notes: "- faster scans",
        publishedAt: "2026-10-20T10:00:00Z",
      },
      available: true,
      checking: true,
      checkedAt: "2026-10-28T10:00:00Z",
      releasesUrl: "https://github.com/FlexEbat/Netscribe/releases",
      changelogUrl: "https://github.com/FlexEbat/Netscribe/releases/tag/v0.3.0",
    },
    helper: { available: true, state: "idle", action: "", message: "", at: null, ...helper },
    auto: null,
  }
}

describe("UpdateDialog", () => {
  it("is not done while the helper still checks the new version, and then it is", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const polls = [
      info("v0.3.0", { state: "running", action: "update" }), // the new version answers, the helper still waits
      info("v0.3.0", { state: "done", action: "update" }),
    ]
    mockFetch({
      "POST /api/update": () => json(200, info("v0.2.0", { state: "waiting", action: "update" })),
      "GET /api/version": () => json(200, polls.shift() ?? info("v0.3.0", { state: "done" })),
    })
    render(<UpdateDialog info={info("v0.2.0")} canUpdate onClose={() => {}} onInfo={() => {}} />)
    await userEvent.click(await screen.findByRole("button", { name: "Update now" }))

    await act(() => vi.advanceTimersByTimeAsync(2600))
    expect(screen.queryByText("Netscribe is updated")).toBeNull()

    await act(() => vi.advanceTimersByTimeAsync(2600))
    expect(await screen.findByText("Netscribe is updated")).toBeTruthy()
  })

  it("reports the rollback, not a success, when the new version is put back", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const polls = [
      info("v0.3.0", { state: "running", action: "update" }),
      info("v0.2.0", { state: "rolled back", action: "update", message: "v0.3.0 did not start" }),
    ]
    mockFetch({
      "POST /api/update": () => json(200, info("v0.2.0", { state: "waiting", action: "update" })),
      "GET /api/version": () =>
        json(200, polls.shift() ?? info("v0.2.0", { state: "rolled back" })),
    })
    render(<UpdateDialog info={info("v0.2.0")} canUpdate onClose={() => {}} onInfo={() => {}} />)
    await userEvent.click(await screen.findByRole("button", { name: "Update now" }))

    await act(() => vi.advanceTimersByTimeAsync(5200))
    expect(screen.queryByText("Netscribe is updated")).toBeNull()
    expect(await screen.findByText("v0.3.0 did not start")).toBeTruthy()
  })
})

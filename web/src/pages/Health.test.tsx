import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import { Health } from "@/pages/Health"
import { json, mockFetch } from "@/test-utils"

afterEach(() => vi.unstubAllGlobals())

const report = {
  worst: "warn",
  ranAt: "2026-11-01T12:00:00Z",
  checks: [
    {
      id: "a",
      status: "ok",
      title: "Config file permissions",
      detail: "only the owner can read it",
      fix: "",
    },
    {
      id: "b",
      status: "warn",
      title: "Free disk space",
      detail: "100 MiB free",
      fix: "Free space.",
    },
  ],
}

describe("Health", () => {
  it("lists problems first, with what to do", async () => {
    mockFetch({ "GET /api/health": () => json(200, report) })
    render(<Health />)
    const items = await screen.findAllByRole("listitem")
    expect(items[0]).toHaveTextContent("Free disk space")
    expect(items[0]).toHaveTextContent("Warning")
    expect(items[0]).toHaveTextContent("Free space.")
    expect(items[1]).toHaveTextContent("Config file permissions")
    expect(screen.getByText(/1 check needs a look/)).toBeInTheDocument()
  })

  it("runs the checks again on request and says when it is too soon", async () => {
    let calls = 0
    mockFetch({
      "GET /api/health": () => json(200, report),
      "POST /api/health/run": () => {
        calls++
        return calls === 1
          ? json(429, { error: "wait" })
          : json(200, { ...report, worst: "ok", checks: [] })
      },
    })
    const user = userEvent.setup()
    render(<Health />)
    await screen.findAllByRole("listitem")
    await user.click(screen.getByRole("button", { name: "Run again" }))
    expect(await screen.findByText(/ran a moment ago/)).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Run again" }))
    expect(await screen.findByText(/Every check is fine/)).toBeInTheDocument()
  })

  it("says so when the checks cannot be loaded", async () => {
    mockFetch({ "GET /api/health": () => json(500, { error: "x" }) })
    render(<Health />)
    expect(await screen.findByText(/could not be loaded/)).toBeInTheDocument()
  })
})

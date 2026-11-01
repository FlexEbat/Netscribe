import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import { SessionsCard } from "@/components/SessionsCard"
import { json, mockFetch } from "@/test-utils"

afterEach(() => vi.unstubAllGlobals())

const here = {
  id: "a1",
  createdAt: "2026-11-01T10:00:00Z",
  lastSeenAt: "2026-11-01T11:00:00Z",
  ip: "10.0.0.5",
  userAgent: "Firefox",
  current: true,
}
const there = {
  id: "b2",
  createdAt: "2026-10-30T10:00:00Z",
  lastSeenAt: "2026-10-31T11:00:00Z",
  ip: "203.0.113.9",
  userAgent: "Chrome",
  current: false,
}

describe("SessionsCard", () => {
  it("marks this session and offers to end only the others", async () => {
    mockFetch({ "GET /api/auth/sessions": () => json(200, [here, there]) })
    render(<SessionsCard />)
    expect(await screen.findByText("This session")).toBeInTheDocument()
    expect(screen.getAllByRole("button", { name: /End the session/ })).toHaveLength(1)
    expect(screen.getByRole("button", { name: "End all other sessions" })).toBeInTheDocument()
  })

  it("ends one session and reloads the list", async () => {
    let list = [here, there]
    const mock = mockFetch({
      "GET /api/auth/sessions": () => json(200, list),
      "DELETE /api/auth/sessions/b2": () => {
        list = [here]
        return new Response(null, { status: 204 })
      },
    })
    const user = userEvent.setup()
    render(<SessionsCard />)
    await user.click(await screen.findByRole("button", { name: /End the session from 203/ }))
    await waitFor(() => expect(screen.queryByText("Chrome")).not.toBeInTheDocument())
    expect(mock.calls.some((c) => c.path === "/api/auth/sessions/b2")).toBe(true)
    expect(screen.queryByRole("button", { name: "End all other sessions" })).not.toBeInTheDocument()
  })

  it("says so when the list cannot be loaded", async () => {
    mockFetch({ "GET /api/auth/sessions": () => json(500, { error: "x" }) })
    render(<SessionsCard />)
    expect(await screen.findByText(/could not be loaded/)).toBeInTheDocument()
  })
})

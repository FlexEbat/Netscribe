import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { BackupCard } from "@/components/BackupCard"
import { json, mockFetch } from "@/test-utils"

afterEach(() => vi.unstubAllGlobals())

beforeEach(() => {
  URL.createObjectURL = vi.fn(() => "blob:test")
  URL.revokeObjectURL = vi.fn()
})

function archive(): Response {
  return new Response("ARCHIVE", {
    status: 200,
    headers: { "Content-Disposition": 'attachment; filename="netscribe-2026-10-31.tar.gz"' },
  })
}

describe("BackupCard", () => {
  it("needs a password before it downloads", async () => {
    mockFetch({})
    render(<BackupCard />)
    expect(screen.getByRole("button", { name: "Download a backup" })).toBeDisabled()
  })

  it("sends the password encoded in a header and saves the file", async () => {
    const mock = mockFetch({ "GET /api/backup": archive })
    const user = userEvent.setup()
    render(<BackupCard />)
    await user.type(screen.getByLabelText("Your password"), "pässword")
    await user.click(screen.getByRole("button", { name: "Download a backup" }))
    expect(await screen.findByText(/The backup was downloaded/)).toBeInTheDocument()
    const call = mock.calls.find((c) => c.path === "/api/backup")
    // UTF-8 bytes of "pässword" in base64, not a Latin-1 string a header could not hold
    expect((call?.init.headers as Record<string, string>)["X-Confirm-Password"]).toBe(
      btoa("p\u00c3\u00a4ssword"),
    )
    expect(URL.createObjectURL).toHaveBeenCalled()
    expect(screen.getByLabelText("Your password")).toHaveValue("")
  })

  it("says so when the password is wrong", async () => {
    mockFetch({ "GET /api/backup": () => json(403, { error: "the password is not right" }) })
    const user = userEvent.setup()
    render(<BackupCard />)
    await user.type(screen.getByLabelText("Your password"), "nope")
    await user.click(screen.getByRole("button", { name: "Download a backup" }))
    expect(await screen.findByText("The password is not right.")).toBeInTheDocument()
  })

  it("asks again before it restores, then reports what was accepted", async () => {
    const mock = mockFetch({
      "POST /api/backup/restore": () =>
        json(202, {
          version: "v0.2.0",
          createdAt: "2026-10-31T12:00:00Z",
          schema: 14,
          devices: 7,
          users: 2,
        }),
    })
    const user = userEvent.setup()
    render(<BackupCard />)
    await user.type(screen.getByLabelText("Your password"), "secret")
    await user.upload(
      screen.getByLabelText("Restore from a backup file"),
      new File(["x"], "mine.tar.gz", { type: "application/gzip" }),
    )
    await user.click(screen.getByRole("button", { name: "Restore" }))
    expect(
      screen.getByText(/replaces the devices, the accounts and the settings/),
    ).toBeInTheDocument()
    expect(mock.calls.some((c) => c.path === "/api/backup/restore")).toBe(false)
    await user.click(screen.getByRole("button", { name: "Replace the data" }))
    await waitFor(() => expect(screen.getByText(/with 7 devices was accepted/)).toBeInTheDocument())
  })

  it("shows the reason a backup was refused", async () => {
    mockFetch({
      "POST /api/backup/restore": () =>
        json(400, { error: "the backup was made by a newer Netscribe than this one" }),
    })
    const user = userEvent.setup()
    render(<BackupCard />)
    await user.type(screen.getByLabelText("Your password"), "secret")
    await user.upload(
      screen.getByLabelText("Restore from a backup file"),
      new File(["x"], "mine.tar.gz"),
    )
    await user.click(screen.getByRole("button", { name: "Restore" }))
    await user.click(screen.getByRole("button", { name: "Replace the data" }))
    expect(await screen.findByText(/newer Netscribe than this one/)).toBeInTheDocument()
  })
})

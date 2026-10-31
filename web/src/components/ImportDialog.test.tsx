import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import { setCsrfToken } from "@/api/client"
import { ImportDialog } from "@/components/ImportDialog"
import { json, mockFetch } from "@/test-utils"

afterEach(() => {
  setCsrfToken("")
  vi.unstubAllGlobals()
})

const file = (text: string) => new File([text], "devices.csv", { type: "text/csv" })

async function choose(text: string) {
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: "Import CSV" }))
  await user.upload(screen.getByLabelText("CSV file"), file(text))
  return user
}

describe("ImportDialog", () => {
  it("checks the file first and imports only after a confirmation", async () => {
    const done = vi.fn()
    const mock = mockFetch({
      "POST /api/devices/import?dryRun=true": () => json(200, { wouldCreate: 2 }),
      "POST /api/devices/import": () => json(201, { created: 2 }),
    })
    render(<ImportDialog onImported={done} />)
    const user = await choose("label\na\nb\n")

    expect(await screen.findByText(/2 devices will be created/)).toBeInTheDocument()
    expect(mock.calls.map((c) => c.path)).toEqual(["/api/devices/import?dryRun=true"])
    expect(done).not.toHaveBeenCalled()

    await user.click(screen.getByRole("button", { name: "Import 2 devices" }))
    await waitFor(() => expect(done).toHaveBeenCalledWith(2))
    expect(mock.calls.map((c) => c.path)).toEqual([
      "/api/devices/import?dryRun=true",
      "/api/devices/import",
    ])
    const sent = mock.calls[1]!.init
    expect(sent.body).toBe("label\na\nb\n")
    expect((sent.headers as Record<string, string>)["Content-Type"]).toContain("text/csv")
  })

  it("lists the lines with a problem and offers no import", async () => {
    mockFetch({
      "POST /api/devices/import?dryRun=true": () =>
        json(400, {
          error: "2 rows have problems",
          rows: [
            { line: 3, error: "ip is not an address" },
            { line: 5, error: 'the group "X" does not exist' },
          ],
        }),
    })
    render(<ImportDialog onImported={() => {}} />)
    await choose("label,ip\na,1\n")
    expect(await screen.findByText("2 rows have problems")).toBeInTheDocument()
    expect(screen.getByText("Line 3: ip is not an address")).toBeInTheDocument()
    expect(screen.getByText('Line 5: the group "X" does not exist')).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Import" })).toBeDisabled()
  })

  it("says so when the server cannot be reached", async () => {
    mockFetch({})
    render(<ImportDialog onImported={() => {}} />)
    await choose("label\na\n")
    expect(await screen.findByText(/not found|could not be checked/)).toBeInTheDocument()
  })
})

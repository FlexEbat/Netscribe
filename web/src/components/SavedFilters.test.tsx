import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import { setCsrfToken, type SavedFilter } from "@/api/client"
import { SavedFilters } from "@/components/SavedFilters"
import { json, mockFetch } from "@/test-utils"

afterEach(() => {
  setCsrfToken("")
  vi.unstubAllGlobals()
})

const saved: SavedFilter[] = [
  {
    id: 2,
    page: "inventory",
    name: "Unknown offline",
    query: "status=offline&trust=unknown",
    createdAt: "2026-10-09T12:00:00Z",
  },
  { id: 1, page: "inventory", name: "Cameras", query: "q=cam", createdAt: "2026-10-08T12:00:00Z" },
]

async function openDialog(
  current: string,
  onApply = vi.fn(),
  extra: Record<string, () => Response> = {},
) {
  const mock = mockFetch({
    "GET /api/filters?page=inventory": () => json(200, { filters: saved }),
    ...extra,
  })
  render(<SavedFilters page="inventory" current={current} onApply={onApply} />)
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: "Saved filters" }))
  return { user, mock, onApply }
}

describe("SavedFilters", () => {
  it("lists the saved filters and applies the chosen one", async () => {
    const { user, onApply } = await openDialog("")
    expect(await screen.findByText("Unknown offline")).toBeInTheDocument()
    expect(screen.getByText("status=offline&trust=unknown")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Apply Cameras" }))
    expect(onApply).toHaveBeenCalledWith("q=cam")
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
  })

  it("will not save when nothing is filtered", async () => {
    await openDialog("")
    expect(await screen.findByLabelText("Name for the current filter")).toBeDisabled()
    expect(screen.getByRole("button", { name: "Save filter" })).toBeDisabled()
    expect(screen.getByText(/Set a search or a filter first/)).toBeInTheDocument()
  })

  it("saves the current filter under the typed name and reloads the list", async () => {
    const { user, mock } = await openDialog("q=cam&status=offline", vi.fn(), {
      "POST /api/filters": () => json(201, { filter: { ...saved[0], id: 3 } }),
    })
    await user.type(await screen.findByLabelText("Name for the current filter"), "Offline cameras")
    await user.click(screen.getByRole("button", { name: "Save filter" }))
    await waitFor(() => expect(mock.calls.some((c) => c.method === "POST")).toBe(true))
    expect(JSON.parse(String(mock.calls.find((c) => c.method === "POST")!.init.body))).toEqual({
      page: "inventory",
      name: "Offline cameras",
      query: "q=cam&status=offline",
    })
    await waitFor(() => expect(mock.calls.filter((c) => c.method === "GET")).toHaveLength(2))
  })

  it("shows why a filter was not saved", async () => {
    const { user } = await openDialog("q=cam", vi.fn(), {
      "POST /api/filters": () => json(409, { error: "you already have a filter with this name" }),
    })
    await user.type(await screen.findByLabelText("Name for the current filter"), "Cameras")
    await user.click(screen.getByRole("button", { name: "Save filter" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("already have a filter")
  })

  it("deletes a filter and loads the list again", async () => {
    const { user, mock } = await openDialog("", vi.fn(), {
      "DELETE /api/filters/1": () => json(204),
    })
    await user.click(await screen.findByRole("button", { name: "Delete filter Cameras" }))
    await waitFor(() => expect(mock.calls.some((c) => c.method === "DELETE")).toBe(true))
    await waitFor(() => expect(mock.calls.filter((c) => c.method === "GET")).toHaveLength(2))
    expect(within(screen.getByRole("dialog")).getByText("Unknown offline")).toBeInTheDocument()
  })
})

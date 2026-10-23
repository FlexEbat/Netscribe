import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { Link } from "@/api/client"
import { LinkBar } from "@/components/LinkBar"
import { json, mockFetch } from "@/test-utils"

const link = (over: Partial<Link> = {}): Link => ({
  id: 7,
  aDeviceId: 1,
  aPort: "eth0",
  bDeviceId: 2,
  bPort: "",
  source: "manual",
  lastSeenAt: "",
  ...over,
})

function bar(l: Link, canEdit = true) {
  const onDeleted = vi.fn()
  render(<LinkBar link={l} aName="router" bName="nas" canEdit={canEdit} onDeleted={onDeleted} />)
  return onDeleted
}

afterEach(() => vi.unstubAllGlobals())

describe("LinkBar", () => {
  it("names both ends, their ports and where the link came from", () => {
    bar(link({ bPort: "lan1" }))
    expect(screen.getByText(/router \(eth0\) to nas \(lan1\), drawn by hand/)).toBeInTheDocument()
  })

  it("offers no delete for a link a scan found", () => {
    bar(link({ source: "lldp" }))
    expect(screen.queryByRole("button", { name: "Delete link" })).not.toBeInTheDocument()
    expect(screen.getByText(/found by lldp/)).toBeInTheDocument()
  })

  it("hides delete from a person who may not edit", () => {
    bar(link(), false)
    expect(screen.queryByRole("button", { name: "Delete link" })).not.toBeInTheDocument()
  })

  it("deletes a hand-drawn link and tells the page", async () => {
    mockFetch({ "DELETE /api/links/7": () => json(204) })
    const onDeleted = bar(link())
    await userEvent.click(screen.getByRole("button", { name: "Delete link" }))
    await vi.waitFor(() => expect(onDeleted).toHaveBeenCalled())
  })

  it("explains a refusal because a scan owns the link", async () => {
    mockFetch({ "DELETE /api/links/7": () => json(409, { error: "link was found by a scan" }) })
    const onDeleted = bar(link())
    await userEvent.click(screen.getByRole("button", { name: "Delete link" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("found by a scan")
    expect(onDeleted).not.toHaveBeenCalled()
  })

  it("gives a plain message for any other failure", async () => {
    mockFetch({ "DELETE /api/links/7": () => json(500, { error: "boom" }) })
    bar(link())
    await userEvent.click(screen.getByRole("button", { name: "Delete link" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("could not be deleted")
  })
})

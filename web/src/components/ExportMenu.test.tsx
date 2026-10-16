import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { ExportMenu } from "@/components/ExportMenu"
import { json, mockFetch } from "@/test-utils"

let saved: { href: string; download: string }[] = []

beforeEach(() => {
  saved = []
  vi.stubGlobal("URL", {
    createObjectURL: () => "blob:fake",
    revokeObjectURL: () => undefined,
  })
  vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    saved.push({ href: this.href, download: this.download })
  })
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

function file(body: string, name: string, type: string) {
  return new Response(body, {
    status: 200,
    headers: { "Content-Type": type, "Content-Disposition": `attachment; filename=${name}` },
  })
}

describe("ExportMenu", () => {
  it("lists an item for every format it is given", async () => {
    const user = userEvent.setup()
    render(<ExportMenu formats={["md", "html", "pdf", "drawio"]} />)
    expect(screen.queryByRole("menuitem")).not.toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "Export" }))
    const items = await screen.findAllByRole("menuitem")
    expect(items.map((i) => i.textContent)).toEqual(["Markdown", "HTML", "PDF", "Draw.io"])
  })

  it("saves the file under the name the server gave it", async () => {
    const mock = mockFetch({
      "GET /api/export/md": () => file("# doc", "netscribe-2026-10-15.md", "text/markdown"),
    })
    const user = userEvent.setup()
    render(<ExportMenu formats={["md"]} />)
    await user.click(screen.getByRole("button", { name: "Export" }))
    await user.click(await screen.findByRole("menuitem", { name: "Markdown" }))

    await waitFor(() => expect(saved).toHaveLength(1))
    expect(saved[0]?.download).toBe("netscribe-2026-10-15.md")
    expect(mock.calls.map((c) => c.path)).toEqual(["/api/export/md"])
    expect(screen.queryByRole("alert")).not.toBeInTheDocument()
  })

  it("shows the message instead of saving a file when the server cannot make one", async () => {
    mockFetch({
      "GET /api/export/pdf": () =>
        json(503, { error: "PDF export needs Chrome or Chromium on the server." }),
    })
    const user = userEvent.setup()
    render(<ExportMenu formats={["pdf"]} />)
    await user.click(screen.getByRole("button", { name: "Export" }))
    await user.click(await screen.findByRole("menuitem", { name: "PDF" }))

    expect(await screen.findByRole("alert")).toHaveTextContent("needs Chrome or Chromium")
    expect(saved).toHaveLength(0)
    expect(screen.getByRole("button", { name: "Export" })).toBeEnabled()
  })

  it("clears the message when the next export starts", async () => {
    let calls = 0
    mockFetch({
      "GET /api/export/pdf": () => {
        calls++
        return calls === 1
          ? json(503, { error: "no chrome" })
          : file("%PDF", "x.pdf", "application/pdf")
      },
    })
    const user = userEvent.setup()
    render(<ExportMenu formats={["pdf"]} />)
    await user.click(screen.getByRole("button", { name: "Export" }))
    await user.click(await screen.findByRole("menuitem", { name: "PDF" }))
    expect(await screen.findByRole("alert")).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "Export" }))
    await user.click(await screen.findByRole("menuitem", { name: "PDF" }))
    await waitFor(() => expect(saved).toHaveLength(1))
    expect(screen.queryByRole("alert")).not.toBeInTheDocument()
  })
})

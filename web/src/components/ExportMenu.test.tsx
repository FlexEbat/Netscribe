import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"
import { ExportMenu } from "@/components/ExportMenu"

describe("ExportMenu", () => {
  it("lists a download link for every format it is given", async () => {
    const user = userEvent.setup()
    render(<ExportMenu formats={["md", "drawio"]} />)
    expect(screen.queryByRole("menuitem")).not.toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "Export" }))
    const items = await screen.findAllByRole("menuitem")
    expect(items.map((i) => i.textContent)).toEqual(["Markdown", "Draw.io"])
    expect(items[0]).toHaveAttribute("href", "/api/export/md")
    expect(items[1]).toHaveAttribute("href", "/api/export/drawio")
    expect(items[0]).toHaveAttribute("download")
  })

  it("names the formats of the later slices too", async () => {
    const user = userEvent.setup()
    render(<ExportMenu formats={["html", "pdf"]} />)
    await user.click(screen.getByRole("button", { name: "Export" }))
    const items = await screen.findAllByRole("menuitem")
    expect(items.map((i) => i.getAttribute("href"))).toEqual([
      "/api/export/html",
      "/api/export/pdf",
    ])
  })
})

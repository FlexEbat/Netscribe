import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import type { NmapState } from "@/api/client"
import { ScanBar } from "@/components/ScanBar"

function bar(extra: Partial<React.ComponentProps<typeof ScanBar>> = {}) {
  const props = {
    scan: null,
    progress: null,
    canRun: true,
    starting: false,
    error: "",
    onStart: vi.fn(),
    onUseNmapChange: vi.fn(),
    ...extra,
  }
  render(<ScanBar {...props} />)
  return props
}

const available: NmapState = { available: true, enabled: false, reason: "" }

describe("ScanBar nmap option", () => {
  it("offers the box when nmap is found and reports a change", async () => {
    const props = bar({ nmap: available })
    const box = screen.getByRole("checkbox", { name: "Identify unknown devices with Nmap" })
    expect(box).not.toBeChecked()
    await userEvent.click(box)
    expect(props.onUseNmapChange).toHaveBeenCalledWith(true)
  })

  it("shows the box as on and locked when the config turns nmap on", () => {
    bar({ nmap: { available: true, enabled: true, reason: "" } })
    const box = screen.getByRole("checkbox", { name: "Nmap (on in the config)" })
    expect(box).toBeChecked()
    expect(box).toBeDisabled()
  })

  it("keeps the box checked after the person ticked it", () => {
    bar({ nmap: available, useNmap: true })
    expect(screen.getByRole("checkbox")).toBeChecked()
  })

  it("hides the box when nmap is missing, unknown or the person cannot run scans", () => {
    for (const extra of [
      { nmap: { available: false, enabled: false, reason: "nmap was not found" } },
      { nmap: null },
      { nmap: available, canRun: false },
    ]) {
      const { unmount } = render(
        <ScanBar
          scan={null}
          progress={null}
          canRun
          starting={false}
          error=""
          onStart={() => undefined}
          {...extra}
        />,
      )
      expect(screen.queryByRole("checkbox")).not.toBeInTheDocument()
      unmount()
    }
  })

  it("locks the box while a scan runs", () => {
    bar({ nmap: available, starting: true })
    expect(screen.getByRole("checkbox")).toBeDisabled()
  })
})

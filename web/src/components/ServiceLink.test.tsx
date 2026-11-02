import { cleanup, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import { ServiceLink } from "@/components/ServiceLink"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const useClipboard = (clipboard: unknown) =>
  Object.defineProperty(navigator, "clipboard", { value: clipboard, configurable: true })

describe("ServiceLink", () => {
  it("copies the ssh command, with -p for another port", async () => {
    const user = userEvent.setup()
    const writeText = vi.fn().mockResolvedValue(undefined)
    useClipboard({ writeText })
    render(<ServiceLink address="192.168.1.9" service={{ port: 2222, name: "ssh" }} />)

    await user.click(screen.getByRole("button", { name: "Copy ssh command" }))
    expect(writeText).toHaveBeenCalledWith("ssh 192.168.1.9 -p 2222")
    expect(await screen.findByRole("button", { name: "Copied" })).toBeInTheDocument()
  })

  it("shows the command to select when the browser has no clipboard", async () => {
    const user = userEvent.setup()
    render(<ServiceLink address="192.168.1.9" service={{ port: 22, name: "ssh" }} />)
    useClipboard(undefined) // userEvent installs its own clipboard, so the stub goes in after setup()

    await user.click(screen.getByRole("button", { name: "Copy ssh command" }))
    expect(await screen.findByText("ssh 192.168.1.9")).toBeInTheDocument()
  })

  it("writes an IPv6 address in brackets and shows nothing for other services", () => {
    render(<ServiceLink address="fd00::5" service={{ port: 8080, name: "" }} />)
    expect(screen.getByRole("link")).toHaveAttribute("href", "http://[fd00::5]:8080")
    cleanup()

    const { container } = render(
      <ServiceLink address="192.168.1.9" service={{ port: 3306, name: "mysql" }} />,
    )
    expect(container).toBeEmptyDOMElement()
  })

  it("makes no link for an address that is not an IP", () => {
    const { container } = render(
      <ServiceLink address={'javascript:alert(1)"'} service={{ port: 80, name: "http" }} />,
    )
    expect(container).toBeEmptyDOMElement()
  })
})

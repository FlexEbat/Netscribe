import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { PortsDialog } from "@/components/PortsDialog"
import { json, mockFetch } from "@/test-utils"

function open() {
  const onDone = vi.fn()
  const onCancel = vi.fn()
  render(
    <PortsDialog
      aDeviceId={1}
      aName="router"
      bDeviceId={2}
      bName="nas"
      onDone={onDone}
      onCancel={onCancel}
    />,
  )
  return { onDone, onCancel }
}

describe("PortsDialog", () => {
  it("sends the trimmed port names and reports success", async () => {
    const fetchMock = mockFetch({ "POST /api/links": () => json(201, { id: 9 }) })
    const { onDone } = open()
    await userEvent.type(screen.getByLabelText("Port on router"), "  eth0 ")
    await userEvent.type(screen.getByLabelText("Port on nas"), "lan1")
    await userEvent.click(screen.getByRole("button", { name: "Create link" }))
    await vi.waitFor(() => expect(onDone).toHaveBeenCalled())
    const init = fetchMock.calls.find((c) => c.method === "POST")?.init
    expect(JSON.parse(String(init?.body))).toEqual({
      aDeviceId: 1,
      aPort: "eth0",
      bDeviceId: 2,
      bPort: "lan1",
    })
  })

  it("lets the user back out", async () => {
    const { onCancel } = open()
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(onCancel).toHaveBeenCalled()
  })

  it.each([
    [409, { error: "exists" }, "This link already exists."],
    [400, { error: "ports are too long" }, "ports are too long"],
    [500, { error: "boom" }, "could not be saved"],
  ])("explains a %i answer", async (status, body, text) => {
    mockFetch({ "POST /api/links": () => json(status, body) })
    const { onDone } = open()
    await userEvent.click(screen.getByRole("button", { name: "Create link" }))
    expect(await screen.findByRole("alert")).toHaveTextContent(text)
    expect(onDone).not.toHaveBeenCalled()
  })
})

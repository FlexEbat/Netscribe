import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { ApiError } from "@/api/client"
import { DeviceForm } from "@/components/DeviceForm"

function form(
  mode: "create" | "edit",
  onSubmit = vi.fn().mockResolvedValue(undefined),
  onReload?: () => void,
) {
  const onCancel = vi.fn()
  render(
    <DeviceForm
      mode={mode}
      initial={{ label: "old", notes: "n" }}
      submitLabel="Save"
      onSubmit={onSubmit}
      onCancel={onCancel}
      onReload={onReload}
    />,
  )
  return { onSubmit, onCancel }
}

describe("DeviceForm", () => {
  it("starts from the given values and submits what was typed", async () => {
    const { onSubmit } = form("create")
    expect(screen.getByLabelText("Name")).toHaveValue("old")
    await userEvent.type(screen.getByLabelText("Hostname"), "box")
    await userEvent.type(screen.getByLabelText("IP address"), "10.0.0.9")
    await userEvent.type(screen.getByLabelText("MAC address"), "aa:bb:cc:dd:ee:ff")
    await userEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(onSubmit).toHaveBeenCalledWith({
      label: "old",
      hostname: "box",
      notes: "n",
      kind: "unknown",
      ip: "10.0.0.9",
      mac: "aa:bb:cc:dd:ee:ff",
    })
  })

  it("leaves the address and hostname out of the edit form", () => {
    form("edit")
    expect(screen.queryByLabelText("IP address")).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Hostname")).not.toBeInTheDocument()
  })

  it("cancels without saving", async () => {
    const { onSubmit, onCancel } = form("edit")
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(onCancel).toHaveBeenCalled()
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it("offers to reload when someone else changed the device", async () => {
    const onReload = vi.fn()
    form("edit", vi.fn().mockRejectedValue(new ApiError(409, "conflict")), onReload)
    await userEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("Someone else changed this device")
    await userEvent.click(screen.getByRole("button", { name: "Load the current data" }))
    expect(onReload).toHaveBeenCalled()
  })

  it("says the address is taken when creating a duplicate", async () => {
    form("create", vi.fn().mockRejectedValue(new ApiError(409, "conflict")))
    await userEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("already exists")
    expect(screen.queryByRole("button", { name: "Load the current data" })).not.toBeInTheDocument()
  })

  it("shows the reason for a rejected value and a plain text for anything else", async () => {
    const onSubmit = vi
      .fn()
      .mockRejectedValueOnce(new ApiError(400, "ip is not an address"))
      .mockRejectedValueOnce(new Error("network"))
    form("create", onSubmit)
    await userEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("ip is not an address")
    await userEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByText(/could not be saved/)).toBeInTheDocument()
  })
})

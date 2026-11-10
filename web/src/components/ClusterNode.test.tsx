import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ReactFlowProvider, type NodeProps } from "@xyflow/react"
import { describe, expect, it, vi } from "vitest"
import type { Device } from "@/api/client"
import { ClusterBox, ClusterCard } from "@/components/ClusterNode"
import type { Cluster } from "@/lib/clusters"

const cluster: Cluster = {
  key: "g1",
  label: "Office",
  vlan: 20,
  devices: [{ id: 1 }, { id: 2 }, { id: 3 }] as Device[],
  containers: 5,
  online: 2,
  kinds: [
    ["host", 2],
    ["router", 1],
  ],
}

function props(expanded: boolean, onToggle: (k: string) => void): NodeProps {
  return { id: "cluster-g1", data: { cluster, expanded, onToggle } } as unknown as NodeProps
}

describe("a closed cluster", () => {
  it("shows its name, the VLAN, the counts and the kinds", () => {
    render(
      <ReactFlowProvider>
        <ClusterCard {...props(false, vi.fn())} />
      </ReactFlowProvider>,
    )
    expect(screen.getByText("Office")).toBeInTheDocument()
    expect(screen.getByText("VLAN 20")).toBeInTheDocument()
    expect(screen.getByText("3 devices, 2 online, 5 containers")).toBeInTheDocument()
  })

  it("opens with a click and says that it is closed", async () => {
    const onToggle = vi.fn()
    render(
      <ReactFlowProvider>
        <ClusterCard {...props(false, onToggle)} />
      </ReactFlowProvider>,
    )
    const button = screen.getByRole("button", { name: "Open Office, 3 devices" })
    expect(button).toHaveAttribute("aria-expanded", "false")
    await userEvent.click(button)
    expect(onToggle).toHaveBeenCalledWith("g1")
  })
})

describe("an open cluster", () => {
  it("closes with its header and says that it is open", async () => {
    const onToggle = vi.fn()
    render(
      <ReactFlowProvider>
        <ClusterBox {...props(true, onToggle)} />
      </ReactFlowProvider>,
    )
    const button = screen.getByRole("button", { name: "Close Office" })
    expect(button).toHaveAttribute("aria-expanded", "true")
    await userEvent.click(button)
    expect(onToggle).toHaveBeenCalledWith("g1")
  })
})

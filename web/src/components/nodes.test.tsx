import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ReactFlowProvider } from "@xyflow/react"
import { describe, expect, it } from "vitest"
import type { Container, Device } from "@/api/client"
import { ContainerNode } from "@/components/ContainerNode"
import { DeviceNode } from "@/components/DeviceNode"
import { TooltipProvider } from "@/components/ui/tooltip"

const device = (over: Partial<Device> = {}): Device => ({
  id: 1,
  mac: "aa:bb:cc:dd:ee:ff",
  ip: "192.168.1.5",
  hostname: "gateway",
  vendor: "Acme",
  kind: "router",
  description: "",
  source: "arp",
  online: true,
  manual: false,
  label: "",
  notes: "",
  osGuess: "",
  kindLocked: false,
  trusted: true,
  groupId: null,
  group: null,
  groupAuto: false,
  version: 1,
  firstSeenAt: "",
  lastSeenAt: "",
  x: 0,
  y: 0,
  ...over,
})

const container = (over: Partial<Container> = {}): Container => ({
  id: "abc",
  deviceId: 1,
  name: "web",
  image: "nginx:1.27",
  state: "running",
  composeProject: "",
  ports: [],
  networks: [],
  firstSeenAt: "",
  lastSeenAt: "",
  ...over,
})

function renderDevice(d: Device, containerCount = 0, selected = false) {
  const props = {
    data: { device: d, containerCount },
    selected,
  } as unknown as React.ComponentProps<typeof DeviceNode>
  return render(
    <ReactFlowProvider>
      <TooltipProvider>
        <DeviceNode {...props} />
      </TooltipProvider>
    </ReactFlowProvider>,
  )
}

describe("DeviceNode", () => {
  it("shows the name, address, state and kind of the device", () => {
    renderDevice(device())
    expect(screen.getByText("gateway")).toBeInTheDocument()
    expect(screen.getByText("192.168.1.5")).toBeInTheDocument()
    expect(screen.getByLabelText("Router")).toBeInTheDocument()
    expect(screen.getByText("Online")).toBeInTheDocument()
  })

  it("says so when the device has no address, and dims an offline one", () => {
    const { container: dom } = renderDevice(device({ ip: "", online: false }))
    expect(screen.getByText("no address")).toBeInTheDocument()
    expect(dom.querySelector(".opacity-60")).not.toBeNull()
  })

  it("marks the selected node", () => {
    const { container: dom } = renderDevice(device(), 0, true)
    expect(dom.querySelector(".ring-2")).not.toBeNull()
  })

  it("shows mac, vendor and container count in the tooltip", async () => {
    renderDevice(device(), 3)
    await userEvent.hover(screen.getByText("gateway"))
    const tip = await screen.findByRole("tooltip")
    expect(tip).toHaveTextContent("aa:bb:cc:dd:ee:ff")
    expect(tip).toHaveTextContent("Acme")
    expect(tip).toHaveTextContent("3 containers")
  })

  it("falls back to the unknown icon for a kind it does not know", () => {
    renderDevice(device({ kind: "toaster" as Device["kind"] }))
    expect(screen.getByText("gateway")).toBeInTheDocument()
  })
})

describe("ContainerNode", () => {
  it("shows name, image and state, and marks the selection", () => {
    const props = {
      data: { container: container(), hostId: "1", index: 0 },
      selected: true,
    } as unknown as React.ComponentProps<typeof ContainerNode>
    const { container: dom } = render(
      <ReactFlowProvider>
        <ContainerNode {...props} />
      </ReactFlowProvider>,
    )
    expect(screen.getByText("web")).toBeInTheDocument()
    expect(screen.getByText("nginx:1.27")).toBeInTheDocument()
    expect(screen.getByText("running")).toBeInTheDocument()
    expect(dom.querySelector(".ring-2")).not.toBeNull()
  })
})

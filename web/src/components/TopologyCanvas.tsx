import { useEffect } from "react"
import {
  Background,
  Controls,
  ConnectionMode,
  MiniMap,
  ReactFlow,
  useEdgesState,
  useNodesState,
  type Edge,
  type Node,
} from "@xyflow/react"
import "@xyflow/react/dist/style.css"
import type { Topology } from "@/api/client"
import { ContainerNode } from "@/components/ContainerNode"
import { DeviceNode } from "@/components/DeviceNode"
import { TooltipProvider } from "@/components/ui/tooltip"
import { useTheme } from "@/lib/theme"
import {
  buildNodes,
  followHost,
  isContainerNode,
  toContainerEdges,
  toEdges,
  type AnyCanvasNode,
} from "@/lib/topology"

const nodeTypes = { device: DeviceNode, container: ContainerNode }

interface TopologyCanvasProps {
  topology: Topology
  onMove: (id: number, x: number, y: number) => void
  onSelect: (id: number | null) => void
  /** A container was clicked, or null when the selection was cleared. */
  onSelectContainer?: (id: string | null) => void
  /** A person dragged a connection from one node to another. */
  onConnectNodes?: (aDeviceId: number, bDeviceId: number) => void
  /** A link was clicked, or null when the selection was cleared. */
  onSelectLink?: (linkId: number | null) => void
  /** Nodes cannot be dragged or connected when the user may not change devices. */
  readOnly?: boolean
}

export function TopologyCanvas({
  topology,
  onMove,
  onSelect,
  onSelectContainer,
  onConnectNodes,
  onSelectLink,
  readOnly = false,
}: TopologyCanvasProps) {
  const { resolved } = useTheme()
  const edgesOf = (t: Topology) => [...toEdges(t.links), ...toContainerEdges(t.containers)]
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>(buildNodes(topology) as Node[])
  const [edges, setEdges] = useEdgesState<Edge>(edgesOf(topology) as Edge[])

  useEffect(() => {
    setNodes((current) => buildNodes(topology, current as AnyCanvasNode[]) as Node[])
    setEdges([...toEdges(topology.links), ...toContainerEdges(topology.containers)] as Edge[])
  }, [topology, setNodes, setEdges])

  return (
    <TooltipProvider delayDuration={300}>
      <div
        className="h-[calc(100vh-10rem)] min-h-96 w-full rounded-lg border"
        data-testid="topology-canvas"
      >
        <ReactFlow
          colorMode={resolved}
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          onNodesChange={onNodesChange}
          nodesDraggable={!readOnly}
          nodesConnectable={!readOnly}
          connectionMode={ConnectionMode.Loose}
          elementsSelectable
          onConnect={(c) => {
            const a = Number(c.source)
            const b = Number(c.target)
            if (Number.isInteger(a) && Number.isInteger(b) && a !== b) onConnectNodes?.(a, b)
          }}
          onNodeClick={(_, node) => {
            onSelectLink?.(null)
            if (isContainerNode(node)) {
              onSelect(null)
              onSelectContainer?.(node.data.container.id)
              return
            }
            onSelectContainer?.(null)
            onSelect(Number(node.id))
          }}
          onEdgeClick={(_, edge) => {
            const linkId = (edge.data as { linkId?: number } | undefined)?.linkId
            if (typeof linkId === "number") onSelectLink?.(linkId)
          }}
          onPaneClick={() => {
            onSelect(null)
            onSelectContainer?.(null)
            onSelectLink?.(null)
          }}
          onNodeDrag={(_, node) => {
            if (!isContainerNode(node)) {
              setNodes(
                (current) =>
                  followHost(current as AnyCanvasNode[], node.id, node.position) as Node[],
              )
            }
          }}
          onNodeDragStop={(_, node) => {
            if (!isContainerNode(node)) onMove(Number(node.id), node.position.x, node.position.y)
          }}
          onlyRenderVisibleElements
          fitView
          minZoom={0.1}
        >
          <Background />
          <MiniMap pannable zoomable />
          <Controls showInteractive={false} />
        </ReactFlow>
      </div>
    </TooltipProvider>
  )
}

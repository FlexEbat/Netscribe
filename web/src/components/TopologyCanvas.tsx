import { useEffect } from "react"
import {
  Background,
  Controls,
  MiniMap,
  ReactFlow,
  useEdgesState,
  useNodesState,
  type Node,
} from "@xyflow/react"
import "@xyflow/react/dist/style.css"
import type { Topology } from "@/api/client"
import { DeviceNode } from "@/components/DeviceNode"
import { TooltipProvider } from "@/components/ui/tooltip"
import { mergeNodes, toEdges, toNodes, type CanvasNode } from "@/lib/topology"

const nodeTypes = { device: DeviceNode }

interface TopologyCanvasProps {
  topology: Topology
  onMove: (id: number, x: number, y: number) => void
  onSelect: (id: number | null) => void
  /** Nodes cannot be dragged when the user may not save positions. */
  readOnly?: boolean
}

export function TopologyCanvas({
  topology,
  onMove,
  onSelect,
  readOnly = false,
}: TopologyCanvasProps) {
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>(toNodes(topology) as Node[])
  const [edges, setEdges] = useEdgesState(toEdges(topology.links))

  useEffect(() => {
    setNodes((current) => mergeNodes(current as CanvasNode[], toNodes(topology)) as Node[])
    setEdges(toEdges(topology.links))
  }, [topology, setNodes, setEdges])

  return (
    <TooltipProvider delayDuration={300}>
      <div
        className="h-[calc(100vh-10rem)] min-h-96 w-full rounded-lg border"
        data-testid="topology-canvas"
      >
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          onNodesChange={onNodesChange}
          nodesDraggable={!readOnly}
          nodesConnectable={false}
          elementsSelectable
          onNodeClick={(_, node) => onSelect(Number(node.id))}
          onPaneClick={() => onSelect(null)}
          onNodeDragStop={(_, node) => onMove(Number(node.id), node.position.x, node.position.y)}
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

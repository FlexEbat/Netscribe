import { useEffect, useMemo } from "react"
import {
  Background,
  ConnectionMode,
  Controls,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  type Edge,
  type Node,
} from "@xyflow/react"
import "@xyflow/react/dist/style.css"
import type { Topology } from "@/api/client"
import { ClusterBox, ClusterCard } from "@/components/ClusterNode"
import { DeviceNode } from "@/components/DeviceNode"
import { TooltipProvider } from "@/components/ui/tooltip"
import { buildClusterGraph, clusterNodeId, type ClusterBy } from "@/lib/clusters"
import { useTheme } from "@/lib/theme"

const nodeTypes = { cluster: ClusterCard, clusterBox: ClusterBox, device: DeviceNode }

interface ClusterCanvasProps {
  topology: Topology
  by: ClusterBy
  expanded: ReadonlySet<string>
  onToggle: (key: string) => void
  /** The device whose panel is open, for the ring around its card. */
  selectedDevice: number | null
  onSelect: (id: number | null) => void
  onSelectLink?: (linkId: number | null) => void
  /** The cluster the visitor opened last: the view moves to it. */
  focus: string | null
}

/** Brings a cluster that was just opened into view. */
function FocusOn({ cluster }: { cluster: string | null }) {
  const { fitView } = useReactFlow()
  useEffect(() => {
    if (cluster === null) return
    const id = window.setTimeout(() => {
      void fitView({ nodes: [{ id: clusterNodeId(cluster) }], padding: 0.15, duration: 300 })
    }, 50)
    return () => window.clearTimeout(id)
  }, [cluster, fitView])
  return null
}

/**
 * The topology as clusters. Positions here are made from the grouping, not read from the
 * server, so nothing can be dragged or connected: the layout of the device view stays as
 * somebody arranged it.
 */
export function ClusterCanvas({
  topology,
  by,
  expanded,
  onToggle,
  selectedDevice,
  onSelect,
  onSelectLink,
  focus,
}: ClusterCanvasProps) {
  const { resolved } = useTheme()
  const graph = useMemo(
    () => buildClusterGraph(topology, by, expanded, onToggle),
    [topology, by, expanded, onToggle],
  )
  const nodes = useMemo(
    () => graph.nodes.map((n) => ({ ...n, selected: n.id === String(selectedDevice) })) as Node[],
    [graph.nodes, selectedDevice],
  )
  const edges = graph.edges as Edge[]
  return (
    <TooltipProvider delayDuration={300}>
      <div
        className="surface h-[calc(100vh-10rem)] min-h-96 w-full overflow-hidden"
        data-testid="cluster-canvas"
      >
        <ReactFlowProvider>
          <ReactFlow
            key={by}
            colorMode={resolved}
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            nodesDraggable={false}
            nodesConnectable={false}
            connectionMode={ConnectionMode.Loose}
            elementsSelectable
            onNodeClick={(_, node) => {
              onSelectLink?.(null)
              if (node.type === "device") onSelect(Number(node.id))
            }}
            onEdgeClick={(_, edge) => {
              const linkId = (edge.data as { linkId?: number } | undefined)?.linkId
              if (typeof linkId === "number") onSelectLink?.(linkId)
            }}
            onPaneClick={() => {
              onSelect(null)
              onSelectLink?.(null)
            }}
            onlyRenderVisibleElements
            fitView
            minZoom={0.05}
          >
            <Background />
            <MiniMap pannable zoomable />
            <Controls showInteractive={false} />
          </ReactFlow>
          <FocusOn cluster={focus} />
        </ReactFlowProvider>
      </div>
    </TooltipProvider>
  )
}

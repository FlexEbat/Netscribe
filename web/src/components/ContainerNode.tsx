import { Cube } from "@phosphor-icons/react"
import { memo } from "react"
import { Handle, Position, type NodeProps } from "@xyflow/react"
import { stateClasses } from "@/lib/containers"
import type { ContainerNodeData } from "@/lib/topology"

/** One container on the canvas: name, image and the state it is in. */
function ContainerNodeView({ data, selected }: NodeProps) {
  const { container } = data as ContainerNodeData
  return (
    <div
      className={`surface flex w-40 flex-col gap-1 rounded-xl px-3 py-2 text-card-foreground ${
        selected ? "ring-2 ring-ring" : ""
      }`}
    >
      <Handle type="target" position={Position.Top} className="!size-2" isConnectable={false} />
      <div className="flex items-center gap-2">
        <Cube className="size-4 shrink-0 text-ok" weight="duotone" aria-hidden />
        <span className="truncate text-xs font-medium">{container.name}</span>
      </div>
      <span className="truncate font-mono readout text-xs text-muted-foreground">
        {container.image}
      </span>
      <span
        className={`w-fit rounded-full border px-2 text-xs font-medium ${stateClasses(container.state)}`}
      >
        {container.state}
      </span>
    </div>
  )
}

export const ContainerNode = memo(ContainerNodeView)

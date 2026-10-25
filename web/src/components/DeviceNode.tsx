import { memo } from "react"
import { Handle, Position, type NodeProps } from "@xyflow/react"
import {
  Camera,
  CircleHelp,
  Cpu,
  HardDrive,
  Monitor,
  Network,
  Printer,
  Router,
  Server,
  Shield,
  Wifi,
  type LucideIcon,
} from "lucide-react"
import type { DeviceKind } from "@/api/client"
import { StatusBadge } from "@/components/StatusBadge"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { displayName, kindLabels, type DeviceNodeData } from "@/lib/topology"

const icons: Record<DeviceKind, LucideIcon> = {
  router: Router,
  switch: Network,
  ap: Wifi,
  firewall: Shield,
  server: Server,
  nas: HardDrive,
  printer: Printer,
  camera: Camera,
  iot: Cpu,
  host: Monitor,
  unknown: CircleHelp,
}

/** One device on the canvas: icon by kind, name, address and state. */
function DeviceNodeView({ data, selected }: NodeProps) {
  const { device, containerCount } = data as DeviceNodeData
  const Icon = icons[device.kind] ?? CircleHelp
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div
          className={`surface surface-lift flex w-48 items-center gap-3 rounded-xl px-3 py-2.5 text-card-foreground ${
            selected ? "ring-2 ring-ring" : ""
          } ${device.online ? "" : "opacity-60"}`}
        >
          <Handle type="target" position={Position.Top} className="!size-2.5" />
          <Handle type="source" position={Position.Bottom} className="!size-2.5" />
          <Icon
            className="size-6 shrink-0 text-muted-foreground"
            aria-label={kindLabels[device.kind]}
          />
          <div className="flex min-w-0 flex-col gap-0.5">
            <span className="truncate text-sm font-medium">{displayName(device)}</span>
            <span className="truncate font-mono text-xs text-muted-foreground">
              {device.ip || "no address"}
            </span>
            <StatusBadge online={device.online} />
          </div>
        </div>
      </TooltipTrigger>
      <TooltipContent>
        <div className="flex flex-col gap-0.5">
          <span>{kindLabels[device.kind]}</span>
          {device.mac ? <span className="font-mono">{device.mac}</span> : null}
          {device.vendor ? <span>{device.vendor}</span> : null}
          {containerCount > 0 ? <span>{containerCount} containers</span> : null}
        </div>
      </TooltipContent>
    </Tooltip>
  )
}

export const DeviceNode = memo(DeviceNodeView)

import {
  Camera,
  Cpu,
  HardDrive,
  HardDrives,
  Monitor,
  Printer,
  Question,
  Network,
  ShareNetwork,
  Shield,
  WifiHigh,
  type Icon,
} from "@phosphor-icons/react"
import { memo, type CSSProperties } from "react"
import { Handle, Position, useStore, type NodeProps } from "@xyflow/react"
import type { DeviceKind } from "@/api/client"
import { StatusBadge } from "@/components/StatusBadge"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { displayName, kindLabels, type DeviceNodeData } from "@/lib/topology"
import { t } from "@/i18n"

export const deviceIcons: Record<DeviceKind, Icon> = {
  router: Network,
  switch: ShareNetwork,
  ap: WifiHigh,
  firewall: Shield,
  server: HardDrives,
  nas: HardDrive,
  printer: Printer,
  camera: Camera,
  iot: Cpu,
  host: Monitor,
  unknown: Question,
}

/** Below this zoom a node is a block with its icon: the text could not be read anyway. */
const FAR_ZOOM = 0.35

/** One device on the canvas: icon by kind, name, address and state. */
function DeviceNodeView({ data, selected }: NodeProps) {
  const { device, containerCount } = data as DeviceNodeData
  const Icon = deviceIcons[device.kind] ?? Question
  const far = useStore((s) => s.transform[2] < FAR_ZOOM)
  if (far) {
    // About the size of the full card, so the layout, the edges and the measured
    // size do not change while the visitor zooms. It has no tooltip and no text: a network
    // of hundreds of devices draws as a map of icons, and the text returns on zoom in.
    return (
      <div
        data-lod="far"
        className={`flex h-[92px] w-52 items-center justify-center rounded-xl ${
          device.online ? "bg-primary/25" : "bg-muted"
        } ${selected ? "ring-4 ring-ring" : ""}`}
      >
        <Handle type="target" position={Position.Top} className="!size-2.5" />
        <Handle type="source" position={Position.Bottom} className="!size-2.5" />
        <Icon className="size-14 text-foreground" aria-label={kindLabels[device.kind]} />
      </div>
    )
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div
          className={`surface surface-lift flex w-52 items-center gap-3 rounded-2xl p-3 text-card-foreground ${
            selected ? "ring-2 ring-ring" : ""
          } ${device.online ? "" : "opacity-60"}`}
        >
          <Handle type="target" position={Position.Top} className="!size-2.5" />
          <Handle type="source" position={Position.Bottom} className="!size-2.5" />
          <span
            aria-hidden="true"
            style={
              {
                "--tile": device.online ? "var(--brand)" : "var(--muted-foreground)",
              } as CSSProperties
            }
            className="nav-tile size-10 rounded-xl"
          >
            <Icon className="size-5" weight="duotone" aria-label={kindLabels[device.kind]} />
          </span>
          <div className="flex min-w-0 flex-col gap-1">
            <span className="truncate text-sm font-semibold">{displayName(device)}</span>
            <span className="readout truncate text-xs text-muted-foreground">
              {device.ip || device.ip6 || t("deviceNode.noAddress")}
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
          {containerCount > 0 ? (
            <span>{t("deviceNode.containers", { n: containerCount })}</span>
          ) : null}
        </div>
      </TooltipContent>
    </Tooltip>
  )
}

export const DeviceNode = memo(DeviceNodeView)

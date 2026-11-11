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
import { memo } from "react"
import { Handle, Position, useStore, type NodeProps } from "@xyflow/react"
import type { DeviceKind } from "@/api/client"
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
        className={`flex h-[92px] w-48 items-center justify-center rounded-sm ${
          device.online ? "border-2 border-primary bg-card" : "border border-dashed bg-muted"
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
          className={`surface surface-lift flex h-[92px] w-48 flex-col justify-between p-3 text-card-foreground ${
            selected ? "ring-2 ring-ring" : ""
          } ${device.online ? "" : "opacity-60"}`}
        >
          <Handle type="target" position={Position.Top} className="!size-2.5" />
          <Handle type="source" position={Position.Bottom} className="!size-2.5" />
          <div className="eyebrow flex items-center justify-between text-muted-foreground">
            <span className="flex items-center gap-2">
              <Icon className="size-4" weight="bold" aria-label={kindLabels[device.kind]} />
              {kindLabels[device.kind]}
            </span>
            <span className="readout">{String(device.id).padStart(3, "0")}</span>
          </div>
          <span className="display truncate text-lg leading-none font-medium">
            {displayName(device)}
          </span>
          <div className="flex items-center justify-between gap-2">
            <span className="readout truncate text-xs text-muted-foreground">
              {device.ip || device.ip6 || t("deviceNode.noAddress")}
            </span>
            <span
              className={`flex items-center gap-2 text-xs ${device.online ? "text-ok" : "text-muted-foreground"}`}
            >
              <span
                aria-hidden="true"
                className={`size-2 ${device.online ? "bg-ok" : "bg-muted-foreground"}`}
              />
              <span className="sr-only">
                {t(device.online ? "statusBadge.online" : "statusBadge.offline")}
              </span>
            </span>
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

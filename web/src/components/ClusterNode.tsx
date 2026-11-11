import { CaretDown, CaretRight } from "@phosphor-icons/react"
import { memo } from "react"
import { Handle, Position, type NodeProps } from "@xyflow/react"
import { deviceIcons } from "@/components/DeviceNode"
import { Badge } from "@/components/ui/badge"
import type { ClusterNodeData } from "@/lib/clusters"
import { kindLabels } from "@/lib/topology"
import { t } from "@/i18n"

/** A cluster that is closed: its name, how many devices and how many are online, the kinds. */
function ClusterCardView({ data }: NodeProps) {
  const { cluster, onToggle } = data as ClusterNodeData
  const total = cluster.devices.length
  return (
    <div className="surface surface-lift h-full w-full rounded-sm text-card-foreground">
      <Handle type="target" position={Position.Top} className="!size-2.5" />
      <Handle type="source" position={Position.Bottom} className="!size-2.5" />
      <button
        type="button"
        aria-expanded={false}
        aria-label={t("cluster.expand", { name: cluster.label, n: total })}
        onClick={() => onToggle(cluster.key)}
        className="flex h-full w-full flex-col justify-between gap-2 rounded-sm p-4 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <span className="flex items-center gap-2">
          <CaretRight className="size-4 shrink-0 text-muted-foreground" aria-hidden />
          <span className="truncate text-base font-semibold">{cluster.label}</span>
          {cluster.vlan ? (
            <Badge variant="outline">{t("cluster.vlan", { n: cluster.vlan })}</Badge>
          ) : null}
        </span>
        <span className="text-sm text-muted-foreground">
          {t("cluster.devicesOnline", { n: total, online: cluster.online })}
          {cluster.containers > 0 ? `, ${t("cluster.containers", { n: cluster.containers })}` : ""}
        </span>
        <span className="flex flex-wrap items-center gap-3">
          {cluster.kinds.slice(0, 4).map(([kind, n]) => {
            const Icon = deviceIcons[kind]
            return (
              <span key={kind} className="flex items-center gap-1 text-sm">
                <Icon className="size-4 text-muted-foreground" aria-label={kindLabels[kind]} />
                {n}
              </span>
            )
          })}
        </span>
      </button>
    </div>
  )
}

/** The frame of a cluster that is open: its devices stand inside it. */
function ClusterBoxView({ data }: NodeProps) {
  const { cluster, onToggle } = data as ClusterNodeData
  return (
    <div className="h-full w-full rounded-sm border border-dashed border-primary/40 bg-primary/5">
      <Handle type="target" position={Position.Top} className="!size-2.5 !opacity-0" />
      <Handle type="source" position={Position.Bottom} className="!size-2.5 !opacity-0" />
      <button
        type="button"
        aria-expanded
        aria-label={t("cluster.collapse", { name: cluster.label })}
        onClick={() => onToggle(cluster.key)}
        className="flex h-[68px] w-full items-center gap-2 rounded-t-2xl px-6 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <CaretDown className="size-4 shrink-0 text-muted-foreground" aria-hidden />
        <span className="truncate text-base font-semibold">{cluster.label}</span>
        {cluster.vlan ? (
          <Badge variant="outline">{t("cluster.vlan", { n: cluster.vlan })}</Badge>
        ) : null}
        <span className="ml-auto text-sm text-muted-foreground">
          {t("cluster.devicesOnline", { n: cluster.devices.length, online: cluster.online })}
        </span>
      </button>
    </div>
  )
}

export const ClusterCard = memo(ClusterCardView)
export const ClusterBox = memo(ClusterBoxView)

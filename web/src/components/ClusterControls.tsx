import { Button } from "@/components/ui/button"
import type { ClusterBy } from "@/lib/clusters"
import { t } from "@/i18n"

export type TopologyView = "devices" | "clusters"

interface ClusterControlsProps {
  view: TopologyView
  onView: (v: TopologyView) => void
  by: ClusterBy
  onBy: (b: ClusterBy) => void
  /** How many clusters are open: the button that closes them all shows only then. */
  openCount: number
  onCollapseAll: () => void
}

function Choice({
  label,
  pressed,
  onClick,
}: {
  label: string
  pressed: boolean
  onClick: () => void
}) {
  return (
    <Button
      type="button"
      size="sm"
      variant={pressed ? "default" : "outline"}
      aria-pressed={pressed}
      onClick={onClick}
    >
      {label}
    </Button>
  )
}

/** Devices or clusters, and what the clusters are made by. */
export function ClusterControls({
  view,
  onView,
  by,
  onBy,
  openCount,
  onCollapseAll,
}: ClusterControlsProps) {
  return (
    <div
      className="flex flex-wrap items-center gap-2"
      role="toolbar"
      aria-label={t("cluster.view")}
    >
      <Choice
        label={t("cluster.devices")}
        pressed={view === "devices"}
        onClick={() => onView("devices")}
      />
      <Choice
        label={t("cluster.clusters")}
        pressed={view === "clusters"}
        onClick={() => onView("clusters")}
      />
      {view === "clusters" ? (
        <>
          <span className="ml-2 text-sm text-muted-foreground">{t("cluster.gatheredBy")}</span>
          <Choice
            label={t("cluster.byGroup")}
            pressed={by === "group"}
            onClick={() => onBy("group")}
          />
          <Choice
            label={t("cluster.byKind")}
            pressed={by === "kind"}
            onClick={() => onBy("kind")}
          />
          {openCount > 0 ? (
            <Button type="button" size="sm" variant="ghost" onClick={onCollapseAll}>
              {t("cluster.collapseAll")}
            </Button>
          ) : null}
        </>
      ) : null}
    </div>
  )
}

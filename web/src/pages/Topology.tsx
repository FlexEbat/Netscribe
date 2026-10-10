import { EmptyState } from "@/components/EmptyState"

export function Topology() {
  return (
    <EmptyState
      title="No scans yet"
      text="Run a scan to discover the devices on your network. The topology appears here."
    />
  )
}

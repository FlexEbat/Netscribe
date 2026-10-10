import { Badge } from "@/components/ui/badge"

export function StatusBadge({ online }: { online: boolean }) {
  return <Badge variant={online ? "default" : "outline"}>{online ? "Online" : "Offline"}</Badge>
}

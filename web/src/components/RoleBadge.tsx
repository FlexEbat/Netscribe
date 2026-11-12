import { roleLabels, type Role } from "@/lib/permissions"

export function RoleBadge({ role }: { role: Role }) {
  return (
    <span className="rounded-full border bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground">
      {roleLabels[role]}
    </span>
  )
}

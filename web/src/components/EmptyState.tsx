import type { ReactNode } from "react"

interface EmptyStateProps {
  title: string
  text: string
  action?: ReactNode
}

export function EmptyState({ title, text, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 py-24 text-center">
      <h2 className="text-lg font-semibold">{title}</h2>
      <p className="max-w-sm text-sm text-muted-foreground">{text}</p>
      {action ? <div className="mt-4">{action}</div> : null}
    </div>
  )
}

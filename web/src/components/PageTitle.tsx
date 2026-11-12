import type { CSSProperties, ReactNode } from "react"
import { navItems, type NavId } from "@/lib/nav"
import { t } from "@/i18n"

interface PageTitleProps {
  page: NavId
  children: ReactNode
}

/** The heading of a page: the icon tile of its menu entry, the title and one line below it. */
export function PageTitle({ page, children }: PageTitleProps) {
  const item = navItems.find((i) => i.id === page)
  if (!item) return <h1 className="text-2xl font-semibold">{children}</h1>
  const Icon = item.icon
  return (
    <div className="flex items-center gap-3">
      <span
        aria-hidden="true"
        style={{ "--tile": item.tint } as CSSProperties}
        className="nav-tile size-12 rounded-2xl"
      >
        <Icon className="size-6" weight="duotone" />
      </span>
      <div className="flex min-w-0 flex-col gap-1">
        <h1 className="text-2xl leading-none font-semibold">{children}</h1>
        <p className="text-sm text-muted-foreground">{t(item.subtitle)}</p>
      </div>
    </div>
  )
}

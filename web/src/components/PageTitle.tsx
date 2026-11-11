import type { ReactNode } from "react"
import { navItems, type NavId } from "@/lib/nav"
import { t } from "@/i18n"

interface PageTitleProps {
  page: NavId
  children: ReactNode
}

/**
 * The title block of a sheet: its number, the title in the display face, one line under it
 * and a rule that is drawn when the page opens.
 */
export function PageTitle({ page, children }: PageTitleProps) {
  const index = navItems.findIndex((i) => i.id === page)
  const item = navItems[index]
  if (!item) return <h1 className="display text-4xl font-medium">{children}</h1>
  return (
    <header className="flex flex-col gap-4">
      <div className="flex flex-col gap-2">
        <p className="eyebrow text-brand">
          {t("pageTitle.sheet", { n: String(index + 1).padStart(2, "0"), total: navItems.length })}
        </p>
        <h1 className="display text-4xl leading-none font-medium md:text-5xl">{children}</h1>
        <p className="max-w-prose text-base text-muted-foreground">{t(item.subtitle)}</p>
      </div>
      <div aria-hidden="true" className="title-rule h-px w-full bg-foreground/40" />
    </header>
  )
}

import { cn } from "@/lib/utils"
import { t } from "@/i18n"

/**
 * The mark of Netscribe: a survey benchmark, a ring with a point and four ticks. It is the
 * only branded element, so it is one component and nothing else draws the logo.
 */
export function BrandMark({ className }: { className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "relative grid size-9 place-items-center border border-foreground bg-card text-foreground",
        className,
      )}
    >
      <svg
        viewBox="0 0 24 24"
        className="size-6"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="square"
      >
        <circle cx="12" cy="12" r="6" />
        <path d="M12 2v5M12 17v5M2 12h5M17 12h5" />
        <circle cx="12" cy="12" r="1.8" fill="var(--primary)" stroke="none" />
      </svg>
    </span>
  )
}

export function Brand({ className }: { className?: string }) {
  return (
    <div className={cn("flex items-center gap-3", className)}>
      <BrandMark />
      <p className="display text-2xl leading-none font-semibold">{t("brand.netscribe")}</p>
    </div>
  )
}

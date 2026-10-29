import { cn } from "@/lib/utils"

/**
 * The mark of Netscribe: a node with three links, drawn with the same thin line as the icons.
 * It is the only branded element, so it is one component and nothing else draws the logo.
 */
export function BrandMark({ className }: { className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "relative grid size-9 place-items-center rounded-xl border bg-muted shadow-[inset_0_1px_0_var(--hairline),0_8px_24px_-12px_var(--glow)]",
        className,
      )}
    >
      <svg
        viewBox="0 0 24 24"
        className="size-5 text-brand"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
      >
        <circle cx="12" cy="12" r="2.6" fill="currentColor" stroke="none" />
        <path d="M12 9.4V4.5M14.3 13.3 18.6 16M9.7 13.3 5.4 16" />
        <circle cx="12" cy="3.6" r="1.4" />
        <circle cx="19.6" cy="16.6" r="1.4" />
        <circle cx="4.4" cy="16.6" r="1.4" />
      </svg>
    </span>
  )
}

export function Brand({ className }: { className?: string }) {
  return (
    <div className={cn("flex items-center gap-3", className)}>
      <BrandMark />
      <p className="text-base font-semibold tracking-tight">Netscribe</p>
    </div>
  )
}

import { Fragment, type ReactNode } from "react"
import { useLang } from "@/i18n"

/**
 * Draws its children again when the language changes. The strings are read when a component
 * renders, so a new key makes the whole panel pick up the new language at once.
 */
export function I18nRoot({ children }: { children: ReactNode }) {
  const lang = useLang()
  return <Fragment key={lang}>{children}</Fragment>
}

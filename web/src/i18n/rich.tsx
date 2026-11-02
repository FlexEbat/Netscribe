import { Fragment, type ReactNode } from "react"
import { t, type Key, type Params } from "@/i18n"

/**
 * Translates a key whose text holds {name} placeholders for elements, such as a code span
 * or a link. The words around the placeholders are the translator's, the elements are the
 * component's: <code>{tRich("key", { cmd: <code>x</code> })}</code>.
 */
export function tRich(key: Key, parts: Record<string, ReactNode>, params?: Params): ReactNode {
  const pieces = t(key, params).split(/(\{\w+\})/)
  return pieces.map((piece, i) => {
    const name = /^\{(\w+)\}$/.exec(piece)?.[1]
    return <Fragment key={i}>{name !== undefined && name in parts ? parts[name] : piece}</Fragment>
  })
}

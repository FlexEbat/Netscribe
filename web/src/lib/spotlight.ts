/**
 * Writes the pointer position into the surface under it, as --mx and --my. The CSS of
 * .surface paints a soft spotlight there. One listener serves the whole page, and the write
 * waits for the next animation frame, so a fast pointer costs one style update per frame.
 */
export function startSpotlight(doc: Document = document): () => void {
  let frame = 0
  let pending: PointerEvent | null = null

  const apply = () => {
    frame = 0
    const event = pending
    pending = null
    const target = event?.target
    if (!event || !(target instanceof Element)) return
    const surface = target.closest<HTMLElement>(".surface")
    if (!surface) return
    const box = surface.getBoundingClientRect()
    surface.style.setProperty("--mx", `${Math.round(event.clientX - box.left)}px`)
    surface.style.setProperty("--my", `${Math.round(event.clientY - box.top)}px`)
  }

  const onMove = (event: PointerEvent) => {
    pending = event
    if (!frame) frame = requestAnimationFrame(apply)
  }

  doc.addEventListener("pointermove", onMove, { passive: true })
  return () => {
    doc.removeEventListener("pointermove", onMove)
    if (frame) cancelAnimationFrame(frame)
  }
}

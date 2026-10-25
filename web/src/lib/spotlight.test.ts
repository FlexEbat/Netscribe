import { afterEach, describe, expect, it, vi } from "vitest"
import { startSpotlight } from "./spotlight"

describe("spotlight", () => {
  afterEach(() => {
    vi.restoreAllMocks()
    document.body.innerHTML = ""
  })

  it("writes the pointer position into the surface under it, relative to its corner", () => {
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((cb) => {
      cb(0)
      return 1
    })
    document.body.innerHTML = `<div class="surface" id="card"><span id="inner">x</span></div>`
    const card = document.getElementById("card")!
    vi.spyOn(card, "getBoundingClientRect").mockReturnValue({ left: 100, top: 40 } as DOMRect)
    const stop = startSpotlight()
    document
      .getElementById("inner")!
      .dispatchEvent(new PointerEvent("pointermove", { bubbles: true, clientX: 160, clientY: 90 }))
    expect(card.style.getPropertyValue("--mx")).toBe("60px")
    expect(card.style.getPropertyValue("--my")).toBe("50px")
    stop()
  })

  it("ignores the pointer outside any surface and stops after cleanup", () => {
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((cb) => {
      cb(0)
      return 1
    })
    document.body.innerHTML = `<div id="plain">x</div><div class="surface" id="card">y</div>`
    const stop = startSpotlight()
    document
      .getElementById("plain")!
      .dispatchEvent(new PointerEvent("pointermove", { bubbles: true, clientX: 5, clientY: 5 }))
    expect(document.getElementById("card")!.style.getPropertyValue("--mx")).toBe("")
    stop()
    document
      .getElementById("card")!
      .dispatchEvent(new PointerEvent("pointermove", { bubbles: true, clientX: 5, clientY: 5 }))
    expect(document.getElementById("card")!.style.getPropertyValue("--mx")).toBe("")
  })

  it("writes once per frame however many moves arrive", () => {
    let frame: FrameRequestCallback | null = null
    const raf = vi.spyOn(window, "requestAnimationFrame").mockImplementation((cb) => {
      frame = cb
      return 1
    })
    document.body.innerHTML = `<div class="surface" id="card">y</div>`
    const stop = startSpotlight()
    const card = document.getElementById("card")!
    for (let i = 0; i < 5; i++)
      card.dispatchEvent(new PointerEvent("pointermove", { bubbles: true, clientX: i, clientY: i }))
    expect(raf).toHaveBeenCalledTimes(1)
    frame!(0)
    stop()
  })
})

import { useEffect, useRef } from "react"
import {
  createField,
  cursorLinks,
  findLinks,
  mulberry32,
  resizeField,
  stepField,
  twinkle,
  type Field,
  type Link,
  type Particle,
} from "@/lib/particles"

const LINK_DISTANCE = 150
const CURSOR_RADIUS = 190
const PARALLAX = 26
const MAX_PULSES = 3

interface Pulse {
  a: Particle
  b: Particle
  start: number
  duration: number
}

/** "176 188 255" from the --particle token, as three numbers. */
function readColor(): [number, number, number] {
  const raw = getComputedStyle(document.documentElement).getPropertyValue("--particle").trim()
  const [r, g, b] = raw.split(/\s+/).map(Number)
  return [r || 176, g || 188, b || 255]
}

/**
 * The atmosphere behind the interface: a few hundred pixels of drifting points, thin lines
 * between the ones that are close, and a faint reaction to the cursor. It paints on one
 * canvas with no layout work, stops while the tab is hidden, and with reduced motion it
 * paints a single still frame.
 */
export function AmbientBackground() {
  const canvasRef = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    let ctx: CanvasRenderingContext2D | null = null
    try {
      ctx = canvas.getContext("2d")
    } catch {
      ctx = null
    }
    if (!ctx) return
    const g: CanvasRenderingContext2D = ctx

    const rng = mulberry32(Math.floor(Math.random() * 2 ** 31))
    const motion = window.matchMedia?.("(prefers-reduced-motion: reduce)")
    let reduced = motion?.matches ?? false
    let color = readColor()
    let width = window.innerWidth
    let height = window.innerHeight
    const field: Field = createField(width, height, rng)
    const pointer = { x: -1e4, y: -1e4, active: false }
    const shift = { x: 0, y: 0 }
    const pulses: Pulse[] = []
    let nextPulse = 1800
    let last = 0
    let raf = 0

    const fit = () => {
      width = window.innerWidth
      height = window.innerHeight
      const dpr = Math.min(window.devicePixelRatio || 1, 2)
      canvas.width = Math.round(width * dpr)
      canvas.height = Math.round(height * dpr)
      g.setTransform(dpr, 0, 0, dpr, 0, 0)
      resizeField(field, width, height, rng)
    }

    const rgba = (alpha: number) =>
      `rgba(${color[0]}, ${color[1]}, ${color[2]}, ${alpha.toFixed(3)})`

    const draw = (t: number) => {
      g.clearRect(0, 0, width, height)
      const at = (p: Particle) => ({ x: p.x + shift.x * p.depth, y: p.y + shift.y * p.depth })

      const links: Link[] = findLinks(field, LINK_DISTANCE)
      g.lineWidth = 1
      for (const l of links) {
        const a = at(l.a)
        const b = at(l.b)
        g.strokeStyle = rgba(l.strength * 0.2)
        g.beginPath()
        g.moveTo(a.x, a.y)
        g.lineTo(b.x, b.y)
        g.stroke()
      }

      if (pointer.active) {
        const glow = g.createRadialGradient(
          pointer.x,
          pointer.y,
          0,
          pointer.x,
          pointer.y,
          CURSOR_RADIUS,
        )
        glow.addColorStop(0, rgba(0.07))
        glow.addColorStop(1, rgba(0))
        g.fillStyle = glow
        g.fillRect(
          pointer.x - CURSOR_RADIUS,
          pointer.y - CURSOR_RADIUS,
          CURSOR_RADIUS * 2,
          CURSOR_RADIUS * 2,
        )
        for (const c of cursorLinks(
          field,
          pointer.x - shift.x,
          pointer.y - shift.y,
          CURSOR_RADIUS,
        )) {
          const p = at(c.particle)
          g.strokeStyle = rgba(c.strength * 0.32)
          g.beginPath()
          g.moveTo(p.x, p.y)
          g.lineTo(pointer.x, pointer.y)
          g.stroke()
        }
      }

      for (const particle of field.particles) {
        const p = at(particle)
        const alpha = 0.55 * particle.depth * twinkle(particle, t)
        g.fillStyle = rgba(alpha)
        g.beginPath()
        g.arc(p.x, p.y, particle.radius, 0, Math.PI * 2)
        g.fill()
        if (particle.depth > 0.85) {
          g.fillStyle = rgba(alpha * 0.14)
          g.beginPath()
          g.arc(p.x, p.y, particle.radius * 4, 0, Math.PI * 2)
          g.fill()
        }
      }

      if (!reduced) {
        const pick = links[Math.floor(rng() * links.length)]
        if (t >= nextPulse && pulses.length < MAX_PULSES && pick) {
          pulses.push({ a: pick.a, b: pick.b, start: t, duration: 1300 + rng() * 900 })
          nextPulse = t + 2200 + rng() * 2600
        }
        for (const pl of pulses.splice(0)) {
          const k = (t - pl.start) / pl.duration
          if (k >= 1) continue
          pulses.push(pl)
          const a = at(pl.a)
          const b = at(pl.b)
          const lerp = (u: number) => ({ x: a.x + (b.x - a.x) * u, y: a.y + (b.y - a.y) * u })
          const head = lerp(k)
          const tail = lerp(Math.max(0, k - 0.18))
          const fade = Math.sin(k * Math.PI)
          g.strokeStyle = rgba(0.55 * fade)
          g.beginPath()
          g.moveTo(tail.x, tail.y)
          g.lineTo(head.x, head.y)
          g.stroke()
          g.fillStyle = rgba(0.9 * fade)
          g.beginPath()
          g.arc(head.x, head.y, 1.6, 0, Math.PI * 2)
          g.fill()
        }
      }
    }

    const frame = (now: number) => {
      const dt = last ? Math.min((now - last) / 1000, 0.05) : 0
      last = now
      stepField(field, dt)
      const ease = 1 - Math.exp(-dt * 2.5)
      const tx = pointer.active ? ((pointer.x - width / 2) / width) * -PARALLAX : 0
      const ty = pointer.active ? ((pointer.y - height / 2) / height) * -PARALLAX : 0
      shift.x += (tx - shift.x) * ease
      shift.y += (ty - shift.y) * ease
      draw(now)
      raf = requestAnimationFrame(frame)
    }

    const start = () => {
      cancelAnimationFrame(raf)
      last = 0
      if (reduced || document.visibilityState === "hidden") {
        if (reduced) draw(0)
        return
      }
      raf = requestAnimationFrame(frame)
    }

    const onMove = (e: PointerEvent) => {
      pointer.x = e.clientX
      pointer.y = e.clientY
      pointer.active = true
    }
    const onLeave = () => {
      pointer.active = false
    }
    const onResize = () => {
      fit()
      if (reduced) draw(0)
    }
    const onMotion = (e: MediaQueryListEvent) => {
      reduced = e.matches
      start()
    }
    const watchTheme = new MutationObserver(() => {
      color = readColor()
      if (reduced) draw(0)
    })

    fit()
    start()
    window.addEventListener("resize", onResize)
    window.addEventListener("pointermove", onMove, { passive: true })
    document.documentElement.addEventListener("mouseleave", onLeave)
    document.addEventListener("visibilitychange", start)
    motion?.addEventListener("change", onMotion)
    watchTheme.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] })

    return () => {
      cancelAnimationFrame(raf)
      window.removeEventListener("resize", onResize)
      window.removeEventListener("pointermove", onMove)
      document.documentElement.removeEventListener("mouseleave", onLeave)
      document.removeEventListener("visibilitychange", start)
      motion?.removeEventListener("change", onMotion)
      watchTheme.disconnect()
    }
  }, [])

  return (
    <canvas
      ref={canvasRef}
      aria-hidden="true"
      className="pointer-events-none fixed inset-0 z-[-1] size-full"
    />
  )
}

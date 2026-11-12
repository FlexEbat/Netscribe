import { describe, expect, it } from "vitest"
import {
  createField,
  cursorLinks,
  findLinks,
  mulberry32,
  particleCount,
  resizeField,
  stepField,
  twinkle,
} from "./particles"

describe("particles", () => {
  it("scales the count with the area and keeps it between the bounds", () => {
    expect(particleCount(100, 100)).toBe(24)
    expect(particleCount(1440, 900)).toBeGreaterThan(24)
    expect(particleCount(1440, 900)).toBeLessThanOrEqual(110)
    expect(particleCount(7680, 4320)).toBe(110)
  })

  it("makes the same field from the same seed", () => {
    const a = createField(1200, 800, mulberry32(7))
    const b = createField(1200, 800, mulberry32(7))
    expect(a.particles).toEqual(b.particles)
    expect(a.particles).toHaveLength(particleCount(1200, 800))
  })

  it("keeps every point on the screen or in the margin, and depth in range", () => {
    const f = createField(1000, 700, mulberry32(3))
    for (let i = 0; i < 600; i++) stepField(f, 0.05)
    for (const p of f.particles) {
      expect(p.x).toBeGreaterThanOrEqual(-40)
      expect(p.x).toBeLessThanOrEqual(1040)
      expect(p.y).toBeGreaterThanOrEqual(-40)
      expect(p.y).toBeLessThanOrEqual(740)
      expect(p.depth).toBeGreaterThanOrEqual(0.4)
      expect(p.depth).toBeLessThanOrEqual(1)
    }
  })

  it("wraps a point that leaves on one side to the other", () => {
    const f = createField(500, 500, mulberry32(1))
    const p = f.particles[0]!
    p.x = 539
    p.vx = 100
    p.y = 250
    p.vy = 0
    stepField(f, 1)
    expect(p.x).toBe(-40)
  })

  it("links only the points that are close, stronger when closer", () => {
    const f = createField(500, 500, mulberry32(1))
    f.particles.length = 3
    const [a, b, c] = f.particles as [
      (typeof f.particles)[0],
      (typeof f.particles)[0],
      (typeof f.particles)[0],
    ]
    Object.assign(a, { x: 0, y: 0, depth: 1 })
    Object.assign(b, { x: 30, y: 0, depth: 1 })
    Object.assign(c, { x: 400, y: 400, depth: 1 })
    const links = findLinks(f, 100)
    expect(links).toHaveLength(1)
    expect(links[0]!.strength).toBeCloseTo(0.7)
    Object.assign(b, { x: 90 })
    expect(findLinks(f, 100)[0]!.strength).toBeCloseTo(0.1)
  })

  it("links the points near the cursor", () => {
    const f = createField(500, 500, mulberry32(1))
    f.particles.length = 2
    Object.assign(f.particles[0]!, { x: 100, y: 100, depth: 1 })
    Object.assign(f.particles[1]!, { x: 400, y: 400, depth: 1 })
    const near = cursorLinks(f, 110, 100, 50)
    expect(near).toHaveLength(1)
    expect(near[0]!.strength).toBeCloseTo(0.8)
  })

  it("adds and drops points when the window changes, and scales positions", () => {
    const rng = mulberry32(5)
    const f = createField(1000, 600, rng)
    const before = f.particles.length
    const p = f.particles[0]!
    const x = p.x
    resizeField(f, 2000, 1200, rng)
    expect(f.particles.length).toBeGreaterThan(before)
    expect(p.x).toBeCloseTo(x * 2)
    resizeField(f, 300, 200, rng)
    expect(f.particles).toHaveLength(24)
  })

  it("twinkles within a narrow band", () => {
    const p = createField(500, 500, mulberry32(2)).particles[0]!
    for (const t of [0, 500, 12345]) {
      expect(twinkle(p, t)).toBeGreaterThanOrEqual(0.7)
      expect(twinkle(p, t)).toBeLessThanOrEqual(1)
    }
  })
})

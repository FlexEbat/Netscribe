/**
 * The field of drifting points behind the interface. This file holds only the numbers, so
 * a test can run it without a canvas: the component draws what these functions return.
 */

export type Rng = () => number

export interface Particle {
  x: number
  y: number
  vx: number
  vy: number
  /** 0.4 to 1. A deep point is small, dim, slow and moves little with the cursor. */
  depth: number
  radius: number
  phase: number
}

export interface Field {
  width: number
  height: number
  particles: Particle[]
}

export interface Link {
  a: Particle
  b: Particle
  /** 0 to 1, falls off with the distance. */
  strength: number
}

const MIN_COUNT = 24
const MAX_COUNT = 110
const AREA_PER_POINT = 17000
const MARGIN = 40

/** A small seeded generator, so a test gets the same field every time. */
export function mulberry32(seed: number): Rng {
  let a = seed >>> 0
  return () => {
    a = (a + 0x6d2b79f5) >>> 0
    let t = a
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

export function particleCount(width: number, height: number): number {
  const n = Math.round((width * height) / AREA_PER_POINT)
  return Math.min(MAX_COUNT, Math.max(MIN_COUNT, n))
}

function between(rng: Rng, lo: number, hi: number): number {
  return lo + (hi - lo) * rng()
}

function spawn(rng: Rng, width: number, height: number, vx: number, vy: number): Particle {
  const depth = between(rng, 0.4, 1)
  return {
    x: between(rng, 0, width),
    y: between(rng, 0, height),
    vx: (vx + between(rng, -2, 2)) * depth,
    vy: (vy + between(rng, -2, 2)) * depth,
    depth,
    radius: between(rng, 0.7, 1.7) * (0.6 + depth * 0.5),
    phase: between(rng, 0, Math.PI * 2),
  }
}

/**
 * Creates the points. About half of them are born in small groups that drift together, which
 * is what makes the web look like a few connected clusters and not like noise.
 */
export function createField(width: number, height: number, rng: Rng): Field {
  const count = particleCount(width, height)
  const particles: Particle[] = []
  const groups = Math.max(2, Math.round(count / 22))
  for (let g = 0; g < groups; g++) {
    const cx = between(rng, 0, width)
    const cy = between(rng, 0, height)
    const gvx = between(rng, -7, 7)
    const gvy = between(rng, -5, 5)
    const size = 4 + Math.floor(rng() * 4)
    for (let i = 0; i < size && particles.length < count; i++) {
      const p = spawn(rng, width, height, gvx, gvy)
      p.x = cx + between(rng, -90, 90)
      p.y = cy + between(rng, -70, 70)
      particles.push(p)
    }
  }
  while (particles.length < count) {
    particles.push(spawn(rng, width, height, between(rng, -5, 5), between(rng, -4, 4)))
  }
  return { width, height, particles }
}

/** Moves every point by dt seconds. A point that leaves the screen comes back on the other side. */
export function stepField(field: Field, dt: number): void {
  const { width, height } = field
  for (const p of field.particles) {
    p.x += p.vx * dt
    p.y += p.vy * dt
    if (p.x < -MARGIN) p.x = width + MARGIN
    else if (p.x > width + MARGIN) p.x = -MARGIN
    if (p.y < -MARGIN) p.y = height + MARGIN
    else if (p.y > height + MARGIN) p.y = -MARGIN
  }
}

/** Fits the field to a new window: positions scale, points are added or dropped. */
export function resizeField(field: Field, width: number, height: number, rng: Rng): void {
  const sx = field.width > 0 ? width / field.width : 1
  const sy = field.height > 0 ? height / field.height : 1
  for (const p of field.particles) {
    p.x *= sx
    p.y *= sy
  }
  field.width = width
  field.height = height
  const count = particleCount(width, height)
  if (field.particles.length > count) field.particles.length = count
  while (field.particles.length < count) {
    field.particles.push(spawn(rng, width, height, between(rng, -5, 5), between(rng, -4, 4)))
  }
}

/** The pairs of points closer than maxDist, with a strength that falls off with the distance. */
export function findLinks(field: Field, maxDist: number): Link[] {
  const out: Link[] = []
  const max2 = maxDist * maxDist
  const ps = field.particles
  for (let i = 0; i < ps.length; i++) {
    const a = ps[i]
    if (!a) continue
    for (let j = i + 1; j < ps.length; j++) {
      const b = ps[j]
      if (!b) continue
      const dx = a.x - b.x
      const dy = a.y - b.y
      const d2 = dx * dx + dy * dy
      if (d2 >= max2) continue
      out.push({ a, b, strength: (1 - Math.sqrt(d2) / maxDist) * Math.min(a.depth, b.depth) })
    }
  }
  return out
}

/** The points within radius of the cursor, with a strength that is highest next to it. */
export function cursorLinks(
  field: Field,
  x: number,
  y: number,
  radius: number,
): { particle: Particle; strength: number }[] {
  const out: { particle: Particle; strength: number }[] = []
  for (const particle of field.particles) {
    const d = Math.hypot(particle.x - x, particle.y - y)
    if (d < radius) out.push({ particle, strength: (1 - d / radius) * particle.depth })
  }
  return out
}

/** The brightness of a point at time t in milliseconds, between 0.7 and 1. */
export function twinkle(p: Particle, t: number): number {
  return 0.85 + 0.15 * Math.sin(t * 0.0009 + p.phase)
}

import { readdirSync, readFileSync, statSync } from "node:fs"
import { join } from "node:path"
import { describe, expect, it } from "vitest"

/*
 * The visual rules of the panel, checked on the source so they cannot drift back:
 * the approved icon sets and fonts, flat backgrounds, Tailwind's type scale and radius,
 * the spacing table, and one easing curve for every transition.
 */

function files(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) files(path, out)
    else out.push(path)
  }
  return out
}

const root = join(__dirname)
const source = files(root).filter((f) => /\.(tsx|ts|css)$/.test(f) && !/\.test\.tsx?$/.test(f))
const markup = source.filter((f) => f.endsWith(".tsx"))
const read = (f: string) => readFileSync(f, "utf8")
const where = (f: string) => f.slice(root.length + 1)

function offenders(files: string[], pattern: RegExp): string[] {
  const out: string[] = []
  for (const f of files) {
    const text = read(f)
    for (const m of text.matchAll(pattern)) out.push(`${where(f)}: ${m[0]}`)
  }
  return out
}

describe("design rules", () => {
  it("takes icons from Phosphor, never from Material or Lucide", () => {
    expect(
      offenders(source, /from "(lucide-react|@mui\/icons-material|material-symbols)[^"]*"/g),
    ).toEqual([])
  })

  it("uses one typeface family and no banned fonts", () => {
    const css = read(join(root, "index.css"))
    // The last entries of a fallback stack are system fonts the visitor already has, so only
    // the font the page asks for first counts.
    const primary = css.match(/--font-sans:\s*"([^"]+)"/)?.[1] ?? ""
    expect(primary).toBe("Geist Variable")
    for (const banned of ["Inter", "Roboto", "Arial", "Open Sans", "Helvetica"]) {
      expect(primary).not.toContain(banned)
    }
  })

  it("never uses italics or the heaviest weights", () => {
    expect(offenders(source, /\b(italic|font-black|font-extrabold|font-\[9\d\d\])\b/g)).toEqual([])
  })

  it("keeps backgrounds flat: a gradient is allowed only on the text of the heading", () => {
    const css = read(join(root, "index.css"))
    const rules = css.split("}")
    const bad = rules.filter(
      (r) => /gradient\(/.test(r) && !/\.hero-heading|\.dark \.hero-heading/.test(r),
    )
    expect(bad).toEqual([])
    expect(
      offenders(
        markup,
        /\b(bg-gradient-[a-z-]+|bg-linear-[a-z0-9-]+|bg-radial[a-z0-9-]*|from-\w+|via-\w+|to-\w+)\b/g,
      ),
    ).toEqual([])
  })

  it("keeps dark backgrounds to the approved set", () => {
    const css = read(join(root, "index.css"))
    const dark = css.slice(css.indexOf(".dark {"), css.indexOf("}", css.indexOf(".dark {")))
    const allowed = new Set(["#000000", "#181818", "#1f1f1f", "#272727", "#313131", "#131209"])
    for (const name of ["background", "card", "popover", "muted", "accent", "input", "sidebar"]) {
      const value = dark.match(new RegExp(`--${name}:\\s*(#[0-9a-fA-F]{6})`))?.[1]?.toLowerCase()
      expect(value, `--${name}`).toBeDefined()
      expect(allowed.has(value ?? ""), `--${name} is ${value}`).toBe(true)
    }
  })

  it("sets no font size outside Tailwind's type scale", () => {
    expect(offenders(markup, /\btext-\[[^\]]*\d(px|rem|em)\]/g)).toEqual([])
    expect(offenders(markup, /\bleading-\[[^\]]+\]/g)).toEqual([])
  })

  it("uses only spacing from the table", () => {
    const bad =
      /(?<![\w[-])-?(p|px|py|pt|pb|pl|pr|ps|pe|m|mx|my|mt|mb|ml|mr|gap|gap-x|gap-y|space-x|space-y|inset|inset-x|inset-y|top|right|bottom|left)-(1\.5|2\.5|3\.5|5|7|9|11|14)(?![\w.\]%/])/g
    expect(offenders(markup, bad)).toEqual([])
    expect(
      offenders(
        markup,
        /(?<![\w[-])-?(p|px|py|pt|pb|pl|pr|m|mx|my|mt|mb|ml|mr|gap)-\[(?!env\()[^\]]+\]/g,
      ),
    ).toEqual([])
  })

  it("uses Tailwind's own radius values", () => {
    const css = read(join(root, "index.css"))
    expect(css).not.toMatch(/--radius(-[a-z0-9]+)?\s*:/)
    expect(offenders(markup, /\brounded(-[a-z]+)?-\[[^\]]+\]/g)).toEqual([])
  })

  it("moves nothing but transform and opacity, on the base curve", () => {
    expect(offenders(markup, /\btransition-all\b/g)).toEqual([])
    expect(offenders(markup, /\b(ease-in-out|ease-linear|ease-in|ease-out)(?![-\w])/g)).toEqual([])
    expect(
      offenders(
        markup,
        /\btransition-\[[^\]]*(width|height|top|left|right|bottom|box-shadow)[^\]]*\]/g,
      ),
    ).toEqual([])
    const css = read(join(root, "index.css"))
    expect(css).toMatch(
      /--default-transition-timing-function:\s*cubic-bezier\(0\.22,\s*1,\s*0\.36,\s*1\)/,
    )
    expect(css).not.toMatch(/animation:[^;]*(?<![-\w])(ease|ease-in-out|linear)(?![-\w])/)
  })

  it("never uses window.alert", () => {
    expect(offenders(markup, /\b(window\.)?alert\(/g)).toEqual([])
  })
})

import { readdirSync, readFileSync, statSync } from "node:fs"
import { join } from "node:path"
import ts from "typescript"
import { describe, expect, it } from "vitest"

/*
 * Every string a person reads comes from the dictionaries. This walks the components, the
 * pages and the shell and reports any text between tags and any literal in an attribute
 * that a person reads, so a new literal fails here and not in a Russian screenshot.
 */

const src = join(__dirname, "..")

function files(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) {
      if (name !== "i18n" && name !== "node_modules") files(path, out)
    } else if (path.endsWith(".tsx") && !/\.test\.tsx$/.test(path)) out.push(path)
  }
  return out
}

const readAttributes = new Set([
  "title",
  "aria-label",
  "aria-description",
  "placeholder",
  "alt",
  "label",
  "description",
  "submitLabel",
  "text",
])

function literals(file: string): string[] {
  const code = readFileSync(file, "utf8")
  const tree = ts.createSourceFile(file, code, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX)
  const found: string[] = []
  const where = (n: ts.Node) =>
    `${file.slice(src.length + 1)}:${tree.getLineAndCharacterOfPosition(n.getStart(tree)).line + 1}`
  const visit = (n: ts.Node) => {
    if (ts.isJsxText(n) && /[A-Za-z]{2}/.test(n.text)) {
      found.push(`${where(n)} text ${JSON.stringify(n.text.trim())}`)
    }
    if (
      ts.isJsxAttribute(n) &&
      readAttributes.has(n.name.getText(tree)) &&
      n.initializer &&
      ts.isStringLiteral(n.initializer) &&
      /[A-Za-z]{2}/.test(n.initializer.text)
    ) {
      found.push(`${where(n)} ${n.name.getText(tree)}=${JSON.stringify(n.initializer.text)}`)
    }
    ts.forEachChild(n, visit)
  }
  visit(tree)
  return found
}

describe("the source of the panel", () => {
  it("holds no literal text that a person reads outside the dictionaries", () => {
    const all = files(src).flatMap(literals)
    expect(all).toEqual([])
  })
})

import { describe, expect, it } from "vitest"
import { can, roleLabels, type Permission } from "./permissions"

describe("can", () => {
  const viewer: Permission[] = ["topology:read", "export:run"]

  it("is true for a permission in the list", () => {
    expect(can(viewer, "topology:read")).toBe(true)
  })

  it("is false for a permission that is missing", () => {
    expect(can(viewer, "users:manage")).toBe(false)
    expect(can(viewer, "scans:run")).toBe(false)
  })

  it("is false for an empty list", () => {
    expect(can([], "topology:read")).toBe(false)
  })
})

describe("roleLabels", () => {
  it("names every role", () => {
    expect(Object.keys(roleLabels).sort()).toEqual(["admin", "operator", "viewer"])
  })
})

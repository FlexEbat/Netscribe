import { describe, expect, it } from "vitest"
import type { Change, Scan } from "@/api/client"
import { countByKind, kindClasses, kindLabels, scanPoints } from "./changes"

function scan(id: number, status: Scan["status"], devices: number): Scan {
  return {
    id,
    target: "192.168.1.0/24",
    status,
    startedAt: "2026-10-09T12:00:00.000Z",
    finishedAt: status === "running" ? null : "2026-10-09T12:01:00.000Z",
    error: status === "failed" ? "boom" : "",
    deviceCount: devices,
    linkCount: 0,
    containerCount: 0,
  }
}

describe("scanPoints", () => {
  it("has one point per completed scan, oldest first", () => {
    const points = scanPoints([scan(3, "done", 9), scan(1, "done", 4), scan(2, "done", 7)])
    expect(points.map((p) => [p.scanId, p.devices])).toEqual([
      [1, 4],
      [2, 7],
      [3, 9],
    ])
  })

  it("leaves out failed and running scans", () => {
    const points = scanPoints([scan(1, "done", 4), scan(2, "failed", 0), scan(3, "running", 0)])
    expect(points.map((p) => p.scanId)).toEqual([1])
  })

  it("is empty without scans", () => {
    expect(scanPoints([])).toEqual([])
  })
})

describe("kinds", () => {
  it("names every kind of change", () => {
    expect(Object.keys(kindLabels)).toHaveLength(8)
    expect(kindLabels.container_removed).toBe("Container removed")
  })

  it("colours what appeared, went away and changed apart", () => {
    const added = kindClasses("device_added")
    const gone = kindClasses("device_offline")
    const changed = kindClasses("device_changed")
    expect(new Set([added, gone, changed]).size).toBe(3)
    expect(kindClasses("link_added")).toBe(added)
    expect(kindClasses("container_removed")).toBe(gone)
    expect(kindClasses("something_else")).toContain("bg-muted")
  })

  it("counts the changes by kind", () => {
    const change = (id: number, kind: Change["kind"]): Change => ({
      id,
      scanId: 1,
      kind,
      entityId: "1",
      summary: "",
      at: "",
    })
    const counts = countByKind([
      change(1, "device_added"),
      change(2, "device_added"),
      change(3, "link_added"),
    ])
    expect(counts.get("device_added")).toBe(2)
    expect(counts.get("link_added")).toBe(1)
    expect(counts.get("device_offline")).toBeUndefined()
  })
})

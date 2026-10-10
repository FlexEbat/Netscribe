import { describe, expect, it } from "vitest"
import { sortByIp } from "./ip"

describe("sortByIp", () => {
  it("orders addresses by value, not as text", () => {
    const sorted = sortByIp([{ ip: "192.168.1.10" }, { ip: "192.168.1.9" }, { ip: "10.0.0.200" }])
    expect(sorted.map((d) => d.ip)).toEqual(["10.0.0.200", "192.168.1.9", "192.168.1.10"])
  })

  it("puts devices without a valid address last and keeps the input intact", () => {
    const input = [{ ip: "" }, { ip: "192.168.1.2" }, { ip: "999.1.1.1" }, { ip: "192.168.1.1" }]
    const sorted = sortByIp(input)
    expect(sorted.map((d) => d.ip)).toEqual(["192.168.1.1", "192.168.1.2", "", "999.1.1.1"])
    expect(input[0]?.ip).toBe("")
  })
})

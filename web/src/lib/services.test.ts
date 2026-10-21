import { describe, expect, it } from "vitest"
import { filterServices, serviceLabel } from "@/lib/services"

const services = [
  { name: "ssh", banner: "", port: 22 },
  { name: "https", banner: "", port: 443 },
  { name: "http-alt", banner: "", port: 8080 },
  { name: "", banner: "", port: 4444 },
]

describe("filterServices by banner", () => {
  const withBanners = [
    { name: "http", banner: "nginx/1.25.3", port: 80 },
    { name: "https", banner: "MikroTik; CN: router.lan", port: 443 },
    { name: "ssh", banner: "", port: 22 },
  ]

  it("finds a service by text in its banner, in any case", () => {
    expect(filterServices(withBanners, "NGINX").map((s) => s.port)).toEqual([80])
    expect(filterServices(withBanners, "router.lan").map((s) => s.port)).toEqual([443])
  })

  it("still finds by name and by port", () => {
    expect(filterServices(withBanners, "ssh").map((s) => s.port)).toEqual([22])
    expect(filterServices(withBanners, "443").map((s) => s.port)).toEqual([443])
  })

  it("does not match a service without a banner on an unrelated word", () => {
    expect(filterServices(withBanners, "apache")).toEqual([])
  })
})

describe("filterServices", () => {
  it("returns everything for an empty or blank query", () => {
    expect(filterServices(services, "")).toHaveLength(4)
    expect(filterServices(services, "   ")).toHaveLength(4)
  })

  it("matches the name as a substring, in any case", () => {
    expect(filterServices(services, "HTTP").map((s) => s.port)).toEqual([443, 8080])
    expect(filterServices(services, "ssh").map((s) => s.port)).toEqual([22])
  })

  it("finds a service without a name by its port", () => {
    expect(filterServices(services, "4444").map((s) => s.port)).toEqual([4444])
  })

  it("does not match a port number as a substring", () => {
    expect(filterServices(services, "44")).toEqual([])
  })

  it("does not change the list it was given", () => {
    const copy = [...services]
    filterServices(services, "ssh")
    expect(services).toEqual(copy)
  })
})

describe("serviceLabel", () => {
  it("falls back to the port", () => {
    expect(serviceLabel({ name: "ssh", port: 22 })).toBe("ssh")
    expect(serviceLabel({ name: "", port: 4444 })).toBe("4444")
  })
})

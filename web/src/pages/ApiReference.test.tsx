import { render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { ApiReference } from "@/pages/ApiReference"
import { json, mockFetch } from "@/test-utils"

afterEach(() => vi.unstubAllGlobals())

const doc = {
  info: { title: "Netscribe API", version: "v1" },
  paths: {
    "/api/devices": {
      get: {
        summary: "Lists devices.",
        description: "Needs the right topology:read. An access token may call it.",
        "x-permission": "topology:read",
        "x-token": true,
        parameters: [{ name: "q", in: "query", description: "text to look for" }],
        responses: { "200": { description: "Success." } },
      },
    },
    "/api/backup": {
      get: {
        summary: "Downloads a backup.",
        description: "Needs the right system:manage. Not available to access tokens.",
        "x-permission": "system:manage",
        "x-token": false,
        responses: { "200": { description: "Success." } },
      },
    },
  },
}

describe("ApiReference", () => {
  it("lists the routes with what each needs, and an example only where a token works", async () => {
    mockFetch({ "GET /api/openapi.json": () => json(200, doc) })
    render(<ApiReference />)
    expect(await screen.findByText("/api/devices")).toBeInTheDocument()
    expect(screen.getByText("Tokens may call it")).toBeInTheDocument()
    expect(screen.getByText("Not for tokens")).toBeInTheDocument()
    expect(screen.getByText(/text to look for/)).toBeInTheDocument()
    const examples = screen.getAllByText(/curl .*NETSCRIBE_TOKEN/)
    expect(examples).toHaveLength(1)
    expect(examples[0]).toHaveTextContent("/api/devices")
  })

  it("says so when the reference cannot be loaded", async () => {
    mockFetch({ "GET /api/openapi.json": () => json(500, { error: "x" }) })
    render(<ApiReference />)
    expect(await screen.findByText(/could not be loaded/)).toBeInTheDocument()
  })
})

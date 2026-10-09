import { afterEach, describe, expect, it, vi } from "vitest"
import { json, mockFetch } from "@/test-utils"
import { ApiError, api, setCsrfToken, setUnauthorizedHandler } from "./client"

afterEach(() => {
  setCsrfToken("")
  setUnauthorizedHandler(null)
  vi.unstubAllGlobals()
})

function header(init: RequestInit, name: string): string | undefined {
  return (init.headers as Record<string, string>)[name]
}

describe("api", () => {
  it("sends the CSRF token on every unsafe method and never on a read", async () => {
    const mock = mockFetch({
      "GET /api/x": () => json(200, {}),
      "POST /api/x": () => json(200, {}),
      "PUT /api/x": () => json(200, {}),
      "PATCH /api/x": () => json(200, {}),
      "DELETE /api/x": () => json(204),
    })
    setCsrfToken("token-1")
    for (const method of ["GET", "POST", "PUT", "PATCH", "DELETE"]) await api(method, "/api/x")

    const sent = Object.fromEntries(
      mock.calls.map((c) => [c.method, header(c.init, "X-CSRF-Token")]),
    )
    expect(sent).toEqual({
      GET: undefined,
      POST: "token-1",
      PUT: "token-1",
      PATCH: "token-1",
      DELETE: "token-1",
    })
  })

  it("sends no token header before one is set", async () => {
    const mock = mockFetch({ "POST /api/x": () => json(200, {}) })
    await api("POST", "/api/x")
    expect(header(mock.calls[0]!.init, "X-CSRF-Token")).toBeUndefined()
  })

  it("encodes a JSON body and uses same-origin credentials", async () => {
    const mock = mockFetch({ "POST /api/x": () => json(200, { ok: true }) })
    const result = await api<{ ok: boolean }>("POST", "/api/x", { body: { a: 1 } })
    const init = mock.calls[0]!.init
    expect(result).toEqual({ ok: true })
    expect(init.body).toBe('{"a":1}')
    expect(header(init, "Content-Type")).toBe("application/json")
    expect(init.credentials).toBe("same-origin")
  })

  it("returns nothing for 204", async () => {
    mockFetch({ "POST /api/x": () => json(204) })
    await expect(api("POST", "/api/x")).resolves.toBeUndefined()
  })

  it("turns an error body into an ApiError with the server's message", async () => {
    mockFetch({ "GET /api/x": () => json(400, { error: "password is shorter than the minimum" }) })
    const error = await api("GET", "/api/x").catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ApiError)
    expect(error).toMatchObject({ status: 400, message: "password is shorter than the minimum" })
  })

  it("falls back to a generic message when the body is not JSON", async () => {
    mockFetch({ "GET /api/x": () => new Response("<html>oops</html>", { status: 502 }) })
    const error = await api("GET", "/api/x").catch((e: unknown) => e)
    expect(error).toMatchObject({ status: 502, message: "Request failed (502)" })
  })

  it("reads Retry-After", async () => {
    mockFetch({
      "POST /api/x": () => json(429, { error: "too many attempts" }, { "Retry-After": "42" }),
    })
    const error = await api("POST", "/api/x").catch((e: unknown) => e)
    expect(error).toMatchObject({ status: 429, retryAfter: 42 })
  })

  it("ignores a Retry-After that is not a number", async () => {
    mockFetch({ "POST /api/x": () => json(429, {}, { "Retry-After": "soon" }) })
    const error = await api("POST", "/api/x").catch((e: unknown) => e)
    expect(error).toMatchObject({ retryAfter: null })
  })

  it("reports a network failure as status 0", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")))
    const error = await api("GET", "/api/x").catch((e: unknown) => e)
    expect(error).toMatchObject({ status: 0, message: "Cannot reach the server" })
  })

  it("calls the unauthorized handler on 401", async () => {
    mockFetch({ "GET /api/x": () => json(401, { error: "authentication required" }) })
    const handler = vi.fn()
    setUnauthorizedHandler(handler)
    await expect(api("GET", "/api/x")).rejects.toBeInstanceOf(ApiError)
    expect(handler).toHaveBeenCalledOnce()
  })

  it("does not call it for a request that expects 401, such as the sign-in", async () => {
    mockFetch({
      "POST /api/auth/login": () => json(401, { error: "invalid username or password" }),
    })
    const handler = vi.fn()
    setUnauthorizedHandler(handler)
    await expect(
      api("POST", "/api/auth/login", { expectUnauthorized: true }),
    ).rejects.toBeInstanceOf(ApiError)
    expect(handler).not.toHaveBeenCalled()
  })

  it("does not call it for other errors", async () => {
    mockFetch({ "GET /api/x": () => json(403, { error: "forbidden" }) })
    const handler = vi.fn()
    setUnauthorizedHandler(handler)
    await expect(api("GET", "/api/x")).rejects.toBeInstanceOf(ApiError)
    expect(handler).not.toHaveBeenCalled()
  })
})

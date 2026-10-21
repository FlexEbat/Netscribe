import { afterEach, describe, expect, it, vi } from "vitest"

import { setUnauthorizedHandler } from "./client"
import { openEvents } from "./events"

class FakeSource {
  static readonly CLOSED = 2
  static instances: FakeSource[] = []
  readyState = 1
  onerror: (() => void) | null = null

  constructor(readonly url: string) {
    FakeSource.instances.push(this)
  }
  addEventListener(): void {}
  close(): void {
    this.readyState = FakeSource.CLOSED
  }
}

afterEach(() => {
  FakeSource.instances = []
  setUnauthorizedHandler(null)
  vi.unstubAllGlobals()
})

function open(status: number): { source: FakeSource; unauthorized: ReturnType<typeof vi.fn> } {
  vi.stubGlobal("EventSource", FakeSource)
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.resolve(new Response("{}", { status }))),
  )
  const unauthorized = vi.fn()
  setUnauthorizedHandler(unauthorized)
  openEvents({})
  const source = FakeSource.instances[0]
  if (!source) throw new Error("no stream was opened")
  return { source, unauthorized }
}

describe("openEvents", () => {
  it("signs out when the refused stream means the session is gone", async () => {
    const { source, unauthorized } = open(401)
    source.readyState = FakeSource.CLOSED
    source.onerror?.()
    await vi.waitFor(() => expect(unauthorized).toHaveBeenCalledTimes(1))
  })

  it("leaves a dropped connection to the browser", () => {
    const { source, unauthorized } = open(401)
    source.readyState = 0 // connecting: the browser retries by itself
    source.onerror?.()
    expect(unauthorized).not.toHaveBeenCalled()
    expect(fetch).not.toHaveBeenCalled()
  })

  it("does not sign out when the session is still valid", async () => {
    const { source, unauthorized } = open(200)
    source.readyState = FakeSource.CLOSED
    source.onerror?.()
    await vi.waitFor(() => expect(fetch).toHaveBeenCalledWith("/api/auth/me", expect.anything()))
    expect(unauthorized).not.toHaveBeenCalled()
  })
})

import { vi } from "vitest"
import type { Permission, Role, User } from "@/lib/permissions"

const allPermissions: Record<Role, Permission[]> = {
  viewer: ["topology:read", "export:run", "tokens:manage"],
  operator: ["topology:read", "export:run", "tokens:manage", "scans:run", "devices:write"],
  admin: [
    "topology:read",
    "export:run",
    "tokens:manage",
    "scans:run",
    "devices:write",
    "audit:read",
    "users:manage",
  ],
}

export function makeUser(overrides: Partial<User> = {}): User {
  return {
    id: 1,
    username: "alice",
    displayName: "",
    role: "viewer",
    disabled: false,
    mustChangePassword: false,
    createdAt: "2026-10-09T12:00:00.000Z",
    lastLoginAt: null,
    ...overrides,
  }
}

export function meBody(user: User, permissions?: Permission[], csrfToken = "csrf-1") {
  return { user, permissions: permissions ?? allPermissions[user.role], csrfToken }
}

export function json(
  status: number,
  body?: unknown,
  headers: Record<string, string> = {},
): Response {
  if (status === 204) return new Response(null, { status })
  return new Response(JSON.stringify(body ?? {}), {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  })
}

type Handler = (init: RequestInit) => Response | Promise<Response>

export interface FetchMock {
  calls: { method: string; path: string; init: RequestInit }[]
}

/** Replaces fetch with handlers keyed by "METHOD /path". Unknown requests get 404. */
export function mockFetch(handlers: Record<string, Handler>): FetchMock {
  const mock: FetchMock = { calls: [] }
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
      const method = (init.method ?? "GET").toUpperCase()
      const path = String(input)
      mock.calls.push({ method, path, init })
      const handler = handlers[`${method} ${path}`]
      return handler ? handler(init) : json(404, { error: "not found" })
    }),
  )
  return mock
}

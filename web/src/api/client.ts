export class ApiError extends Error {
  readonly status: number
  /** Seconds to wait, from a Retry-After header. */
  readonly retryAfter: number | null

  constructor(status: number, message: string, retryAfter: number | null = null) {
    super(message)
    this.name = "ApiError"
    this.status = status
    this.retryAfter = retryAfter
  }
}

let csrfToken = ""
let onUnauthorized: (() => void) | null = null

/** The session's CSRF token, sent with every request that can change state. */
export function setCsrfToken(token: string): void {
  csrfToken = token
}

/** Called when a request that needed a session gets 401, so the interface can sign out. */
export function setUnauthorizedHandler(handler: (() => void) | null): void {
  onUnauthorized = handler
}

const safeMethods = new Set(["GET", "HEAD", "OPTIONS"])

interface Options {
  body?: unknown
  /** Requests that are allowed to answer 401 without ending the session, such as the sign-in itself. */
  expectUnauthorized?: boolean
}

export async function api<T = void>(
  method: string,
  path: string,
  { body, expectUnauthorized = false }: Options = {},
): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" }
  if (body !== undefined) headers["Content-Type"] = "application/json"
  if (!safeMethods.has(method.toUpperCase()) && csrfToken) headers["X-CSRF-Token"] = csrfToken

  let response: Response
  try {
    response = await fetch(path, {
      method,
      headers,
      credentials: "same-origin",
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError(0, "Cannot reach the server")
  }

  if (response.status === 401 && !expectUnauthorized) onUnauthorized?.()
  if (!response.ok) {
    throw new ApiError(response.status, await errorMessage(response), retryAfter(response))
  }
  if (response.status === 204) return undefined as T
  return (await response.json()) as T
}

async function errorMessage(response: Response): Promise<string> {
  try {
    const data: unknown = await response.json()
    if (
      typeof data === "object" &&
      data !== null &&
      "error" in data &&
      typeof data.error === "string"
    ) {
      return data.error
    }
  } catch {
    // The body is not JSON: fall through to the generic text.
  }
  return `Request failed (${response.status})`
}

function retryAfter(response: Response): number | null {
  const value = Number(response.headers.get("Retry-After"))
  return Number.isFinite(value) && value > 0 ? value : null
}

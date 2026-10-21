import { useState } from "react"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { ErrorBoundary } from "@/components/ErrorBoundary"

let broken = true

function Page() {
  if (broken) throw new Error("secret detail from the server")
  return <p>Page content</p>
}

function Host() {
  const [, setN] = useState(0)
  return (
    <>
      <button onClick={() => setN((n) => n + 1)}>Rerender</button>
      <ErrorBoundary>
        <Page />
      </ErrorBoundary>
    </>
  )
}

describe("ErrorBoundary", () => {
  beforeEach(() => {
    broken = true
    vi.spyOn(console, "error").mockImplementation(() => undefined)
  })
  afterEach(() => vi.restoreAllMocks())

  it("shows a retry button instead of the crashed page, without the error text", () => {
    render(<Host />)
    expect(screen.getByText("Something went wrong")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument()
    expect(screen.queryByText(/secret detail/)).not.toBeInTheDocument()
  })

  it("renders the page again after Try again once the cause is gone", async () => {
    render(<Host />)
    broken = false
    await userEvent.click(screen.getByRole("button", { name: "Try again" }))
    expect(screen.getByText("Page content")).toBeInTheDocument()
  })

  it("renders its children when nothing fails", () => {
    broken = false
    render(<Host />)
    expect(screen.getByText("Page content")).toBeInTheDocument()
  })
})

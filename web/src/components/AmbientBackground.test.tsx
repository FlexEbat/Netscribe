import { render } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { AmbientBackground } from "./AmbientBackground"
import { Brand } from "./Brand"

describe("AmbientBackground", () => {
  it("is a decorative canvas that no assistive technology reads", () => {
    const { container } = render(<AmbientBackground />)
    const canvas = container.querySelector("canvas")
    expect(canvas).not.toBeNull()
    expect(canvas).toHaveAttribute("aria-hidden", "true")
    expect(canvas?.className).toContain("pointer-events-none")
  })

  it("does nothing, and does not throw, where there is no canvas context", () => {
    expect(() => render(<AmbientBackground />).unmount()).not.toThrow()
  })
})

describe("Brand", () => {
  it("names the product and hides the mark from readers", () => {
    const { getByText, container } = render(<Brand />)
    expect(getByText("Netscribe")).toBeInTheDocument()
    expect(container.querySelector('[aria-hidden="true"]')).not.toBeNull()
  })
})

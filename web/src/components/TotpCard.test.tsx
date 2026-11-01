import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import { TotpCard } from "@/components/TotpCard"
import { json, mockFetch } from "@/test-utils"

afterEach(() => vi.unstubAllGlobals())

describe("TotpCard", () => {
  it("sets up, confirms with a code and shows the recovery codes once", async () => {
    let enabled = false
    const mock = mockFetch({
      "GET /api/auth/totp": () => json(200, { enabled, recoveryCodesLeft: enabled ? 10 : 0 }),
      "POST /api/auth/totp/setup": () => json(200, { secret: "ABCDEFGH", uri: "otpauth://totp/x" }),
      "POST /api/auth/totp/enable": () => {
        enabled = true
        return json(200, { recoveryCodes: ["AAAAABBBBB", "CCCCCDDDDD"] })
      },
    })
    const user = userEvent.setup()
    render(<TotpCard />)
    await user.type(await screen.findByLabelText("Your password"), "secret")
    await user.click(screen.getByRole("button", { name: "Set up a second factor" }))
    expect(await screen.findByText("ABCDEFGH")).toBeInTheDocument()
    await user.type(screen.getByLabelText("Code from the app"), "123456")
    await user.click(screen.getByRole("button", { name: "Turn on the second factor" }))
    expect(await screen.findByText("AAAAABBBBB")).toBeInTheDocument()
    expect(await screen.findByText(/The second factor is on/)).toBeInTheDocument()
    expect(
      JSON.parse(String(mock.calls.find((c) => c.path === "/api/auth/totp/enable")?.init.body)),
    ).toEqual({
      code: "123456",
    })
  })

  it("turns it off with the password and a code", async () => {
    let enabled = true
    const mock = mockFetch({
      "GET /api/auth/totp": () => json(200, { enabled, recoveryCodesLeft: 1 }),
      "DELETE /api/auth/totp": () => {
        enabled = false
        return new Response(null, { status: 204 })
      },
    })
    const user = userEvent.setup()
    render(<TotpCard />)
    expect(await screen.findByText(/1 recovery code is left/)).toBeInTheDocument()
    await user.type(screen.getByLabelText("Your password"), "secret")
    await user.type(screen.getByLabelText("A current code or a recovery code"), "654321")
    await user.click(screen.getByRole("button", { name: "Turn off the second factor" }))
    expect(
      await screen.findByRole("button", { name: "Set up a second factor" }),
    ).toBeInTheDocument()
    expect(mock.calls.some((c) => c.method === "DELETE" && c.path === "/api/auth/totp")).toBe(true)
  })

  it("says so when the password is wrong", async () => {
    mockFetch({
      "GET /api/auth/totp": () => json(200, { enabled: false, recoveryCodesLeft: 0 }),
      "POST /api/auth/totp/setup": () => json(403, { error: "the password is not right" }),
    })
    const user = userEvent.setup()
    render(<TotpCard />)
    await user.type(await screen.findByLabelText("Your password"), "nope")
    await user.click(screen.getByRole("button", { name: "Set up a second factor" }))
    expect(await screen.findByText("The password or the code is not right.")).toBeInTheDocument()
  })
})

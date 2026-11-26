import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { TlsStatus } from "@/api/client"
import { TlsCard } from "@/components/TlsCard"
import { json, mockFetch } from "@/test-utils"

afterEach(() => vi.unstubAllGlobals())

const off: TlsStatus = {
  enabled: false,
  selfSigned: false,
  subject: "",
  names: [],
  notBefore: "",
  notAfter: "",
  fingerprint: "",
}
const on = {
  enabled: true,
  selfSigned: true,
  subject: "CN=panel.lan",
  names: ["panel.lan", "10.0.0.5"],
  notBefore: "2026-11-26T11:55:00Z",
  notAfter: "2027-11-26T12:00:00Z",
  fingerprint: "AA:BB:CC",
}
const made = (needRestart: boolean) => ({
  subject: "CN=panel.lan",
  names: ["panel.lan"],
  notAfter: "2027-11-26T12:00:00Z",
  fingerprint: "11:22:33",
  needRestart,
})

describe("TlsCard", () => {
  it("says the panel is on plain http when no certificate is set", async () => {
    mockFetch({ "GET /api/tls": () => json(200, off) })
    render(<TlsCard canRestart />)
    expect(await screen.findByText(/served over plain http/)).toBeInTheDocument()
  })

  it("shows the subject, the names and the fingerprint to compare", async () => {
    mockFetch({ "GET /api/tls": () => json(200, on) })
    render(<TlsCard canRestart />)
    expect(await screen.findByText("AA:BB:CC")).toBeInTheDocument()
    expect(screen.getByText(/CN=panel.lan/)).toBeInTheDocument()
    expect(screen.getByText("panel.lan, 10.0.0.5")).toBeInTheDocument()
    expect(screen.getByText(/signed by itself/)).toBeInTheDocument()
    expect(
      screen.getByText(/A browser warns about a certificate signed by its own key/),
    ).toBeInTheDocument()
  })

  it("needs the password, sends it in the header and clears it", async () => {
    let state = off
    const mock = mockFetch({
      "GET /api/tls": () => json(200, state),
      "POST /api/tls/self-signed": () => {
        state = on
        return json(200, made(true))
      },
    })
    const user = userEvent.setup()
    render(<TlsCard canRestart />)
    const button = await screen.findByRole("button", { name: "Make a certificate" })
    await waitFor(() => expect(button).toBeDisabled())
    await user.clear(screen.getByLabelText("Names"))
    await user.type(screen.getByLabelText("Names"), "panel.lan, 10.0.0.5")
    await user.type(screen.getByLabelText("Your password"), "pässword")
    await user.click(button)
    expect(await screen.findByText(/Compare this fingerprint/)).toBeInTheDocument()
    const call = mock.calls.find((c) => c.path === "/api/tls/self-signed")
    expect((call?.init.headers as Record<string, string>)["X-Confirm-Password"]).toBe(
      btoa("p\u00c3\u00a4ssword"),
    )
    expect(JSON.parse(String(call?.init.body))).toEqual({
      names: ["panel.lan", "10.0.0.5"],
      days: 365,
      replace: false,
    })
    expect(screen.getByLabelText("Your password")).toHaveValue("")
    // The card now shows the new certificate and offers a replacement.
    expect(
      await screen.findByRole("button", { name: "Make a new certificate" }),
    ).toBeInTheDocument()
  })

  it("sends replace when a certificate is set already", async () => {
    const mock = mockFetch({
      "GET /api/tls": () => json(200, on),
      "POST /api/tls/self-signed": () => json(200, made(false)),
    })
    const user = userEvent.setup()
    render(<TlsCard canRestart />)
    await screen.findByText("AA:BB:CC")
    await user.type(screen.getByLabelText("Your password"), "secret")
    await user.click(screen.getByRole("button", { name: "Make a new certificate" }))
    await screen.findByText(/Compare this fingerprint/)
    const call = mock.calls.find((c) => c.path === "/api/tls/self-signed")
    expect(JSON.parse(String(call?.init.body)).replace).toBe(true)
  })

  it("shows Restart now only when the answer says needRestart", async () => {
    mockFetch({
      "GET /api/tls": () => json(200, off),
      "POST /api/tls/self-signed": () => json(200, made(false)),
    })
    const user = userEvent.setup()
    render(<TlsCard canRestart />)
    await screen.findByText(/served over plain http/)
    await user.type(screen.getByLabelText("Your password"), "secret")
    await user.click(screen.getByRole("button", { name: "Make a certificate" }))
    await screen.findByText(/Compare this fingerprint/)
    expect(screen.queryByRole("button", { name: "Restart now" })).not.toBeInTheDocument()
  })

  it("offers Restart now with a helper, and calls the restart route", async () => {
    const mock = mockFetch({
      "GET /api/tls": () => json(200, off),
      "POST /api/tls/self-signed": () => json(200, made(true)),
      "POST /api/system/restart": () => json(202, {}),
    })
    const user = userEvent.setup()
    render(<TlsCard canRestart />)
    await screen.findByText(/served over plain http/)
    await user.type(screen.getByLabelText("Your password"), "secret")
    await user.click(screen.getByRole("button", { name: "Make a certificate" }))
    await user.click(await screen.findByRole("button", { name: "Restart now" }))
    await waitFor(() => expect(mock.calls.some((c) => c.path === "/api/system/restart")).toBe(true))
    expect(screen.queryByRole("button", { name: "Restart now" })).not.toBeInTheDocument()
  })

  it("shows the command when there is no helper", async () => {
    mockFetch({
      "GET /api/tls": () => json(200, off),
      "POST /api/tls/self-signed": () => json(200, made(true)),
    })
    const user = userEvent.setup()
    render(<TlsCard canRestart={false} />)
    await screen.findByText(/served over plain http/)
    await user.type(screen.getByLabelText("Your password"), "secret")
    await user.click(screen.getByRole("button", { name: "Make a certificate" }))
    expect(await screen.findByText("sudo systemctl restart netscribe")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Restart now" })).not.toBeInTheDocument()
  })

  it("says so when the password is wrong, and shows the server's refusal otherwise", async () => {
    let answer = () => json(403, { error: "the password is not right" })
    mockFetch({
      "GET /api/tls": () => json(200, off),
      "POST /api/tls/self-signed": () => answer(),
    })
    const user = userEvent.setup()
    render(<TlsCard canRestart />)
    await screen.findByText(/served over plain http/)
    await user.type(screen.getByLabelText("Your password"), "nope")
    await user.click(screen.getByRole("button", { name: "Make a certificate" }))
    expect(await screen.findByText("The password is not right.")).toBeInTheDocument()

    answer = () => json(400, { error: '"bad_name!": name is not a host name or an address' })
    await user.type(screen.getByLabelText("Your password"), "secret")
    await user.click(screen.getByRole("button", { name: "Make a certificate" }))
    expect(await screen.findByText(/not a host name or an address/)).toBeInTheDocument()
  })

  it("reports a failed load", async () => {
    mockFetch({})
    render(<TlsCard canRestart />)
    expect(await screen.findByText("The certificate could not be read.")).toBeInTheDocument()
  })
})

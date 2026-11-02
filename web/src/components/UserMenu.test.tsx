import { cleanup, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router-dom"
import { afterEach, describe, expect, it } from "vitest"
import { UserMenu } from "@/components/UserMenu"
import { langStorageKey, resetLang, setLang } from "@/i18n"
import { I18nRoot } from "@/i18n/root"

const user = {
  id: 1,
  username: "olga",
  displayName: "",
  role: "operator" as const,
  disabled: false,
  mustChangePassword: false,
  createdAt: "2026-10-01T00:00:00.000Z",
  lastLoginAt: null,
}

afterEach(() => {
  cleanup()
  localStorage.clear()
  resetLang()
})

describe("UserMenu language", () => {
  it("switches the panel to Russian, remembers it and sets the lang of the page", async () => {
    setLang("en")
    render(
      <MemoryRouter>
        <I18nRoot>
          <UserMenu user={user} onSignOut={() => {}} />
        </I18nRoot>
      </MemoryRouter>,
    )
    await userEvent.click(screen.getByRole("button", { name: /olga/ }))
    await userEvent.click(await screen.findByRole("menuitemradio", { name: "Русский" }))

    expect(localStorage.getItem(langStorageKey)).toBe("ru")
    expect(document.documentElement.lang).toBe("ru")

    await userEvent.click(screen.getByRole("button", { name: /olga/ }))
    expect(await screen.findByRole("menuitem", { name: "Выйти" })).toBeInTheDocument()
  })
})

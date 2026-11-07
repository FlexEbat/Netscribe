import { expect, test } from "@playwright/test"
import { devUser, openUserMenu, signIn, signInAndWait } from "./helpers"

test("a wrong password is refused and the form stays", async ({ page }) => {
  await signIn(page, devUser, "not-the-password-1")
  await expect(page.getByText("Invalid username or password.")).toBeVisible()
  await expect(page).toHaveURL(/\/login$/)
})

test("the right password opens the panel, sign out returns to the form", async ({ page }) => {
  await signInAndWait(page)
  await openUserMenu(page)
  await page.getByRole("menuitem", { name: "Sign out" }).click()
  await expect(page.getByRole("button", { name: "Sign in" })).toBeVisible()
  await page.goto("/")
  await expect(page).toHaveURL(/\/login/)
})

test("a page behind the session sends an anonymous visitor to sign in", async ({ page }) => {
  await page.goto("/inventory")
  await expect(page).toHaveURL(/\/login/)
})

import { expect, type Page } from "@playwright/test"

// The account that cmd/seed creates with --dev-user. scripts/e2e.sh sets the password.
export const devUser = "dev"
export const devPassword = process.env.NETSCRIBE_DEV_PASSWORD ?? "e2e-dev-password-12"

export async function signIn(page: Page, username = devUser, password = devPassword) {
  await page.goto("/login")
  await page.getByLabel("Username").fill(username)
  await page.getByLabel("Password").fill(password)
  await page.getByRole("button", { name: "Sign in" }).click()
}

export async function signInAndWait(page: Page) {
  await signIn(page)
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible()
}

export async function openUserMenu(page: Page) {
  await page.getByRole("button", { name: new RegExp(`^${devUser}`) }).click()
}

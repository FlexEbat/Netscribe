import { expect, test } from "@playwright/test"
import { openUserMenu, signInAndWait } from "./helpers"

test("the language switch changes the panel and the page language", async ({ page }) => {
  await signInAndWait(page)
  await openUserMenu(page)
  await page.getByRole("menuitemradio", { name: "Русский" }).click()
  await expect(page.locator("html")).toHaveAttribute("lang", "ru")
  await expect(page.getByRole("heading", { name: "Overview" })).toHaveCount(0)

  await openUserMenu(page)
  await page.getByRole("menuitemradio", { name: "English" }).click()
  await expect(page.locator("html")).toHaveAttribute("lang", "en")
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible()
})

test.describe("a phone width of 390 pixels", () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test("the sign in page and the overview do not scroll sideways", async ({ page }) => {
    await page.goto("/login")
    await expect(page.getByRole("button", { name: "Sign in" })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390)

    await signInAndWait(page)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390)
  })
})

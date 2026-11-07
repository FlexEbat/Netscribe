import { expect, test } from "@playwright/test"
import { signInAndWait } from "./helpers"

test("the trust filter separates unknown devices from trusted ones", async ({ page }) => {
  await signInAndWait(page)
  await page.goto("/inventory")
  await expect(page.getByText("gateway").first()).toBeVisible()

  await page.getByLabel("Filter by trust").selectOption("unknown")
  await expect(page.getByText("gateway").first()).toBeVisible()

  // nobody has trusted the demo devices
  await page.getByLabel("Filter by trust").selectOption("trusted")
  await expect(page.getByText("gateway")).toHaveCount(0)
})

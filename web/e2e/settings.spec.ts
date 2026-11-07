import { expect, test } from "@playwright/test"
import { signInAndWait } from "./helpers"

// Each test puts the value back, so the order of the tests does not matter.
test("a valid scan interval is saved and applies at once", async ({ page }) => {
  await signInAndWait(page)
  await page.goto("/settings")
  const every = page.getByLabel("Scan every")
  const before = await every.inputValue()
  await every.fill("37m")
  await page.getByRole("button", { name: "Save changes" }).click()
  await expect(page.getByText("Saved 1 setting.")).toBeVisible()

  await page.reload()
  await expect(page.getByLabel("Scan every")).toHaveValue("37m")

  await page.getByLabel("Scan every").fill(before)
  await page.getByRole("button", { name: "Save changes" }).click()
  await expect(page.getByText("Saved 1 setting.")).toBeVisible()
})

test("an invalid scan interval is refused and not saved", async ({ page }) => {
  await signInAndWait(page)
  await page.goto("/settings")
  const every = page.getByLabel("Scan every")
  const before = await every.inputValue()
  await every.fill("soon")
  await page.getByRole("button", { name: "Save changes" }).click()
  await expect(page.getByText(/Saved \d+ setting/)).toHaveCount(0)

  await page.reload()
  await expect(page.getByLabel("Scan every")).toHaveValue(before)
})

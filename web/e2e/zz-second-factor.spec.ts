import { expect, test } from "@playwright/test"
import { devPassword, openUserMenu, signIn, signInAndWait, totpCode } from "./helpers"

// The file name sorts last on purpose: with one worker the files run in name order, and if this
// test fails halfway the account keeps its second factor, which must not break the other specs.
// The whole life of a second factor on the account "dev": turn it on, sign in with a
// recovery code, turn it off. The account ends the way it began, so the other specs do not
// notice it.
test("the second factor can be turned on, asked at sign in and turned off", async ({ page }) => {
  await signInAndWait(page)
  await page.goto("/account")
  await page.locator("#totp-password").fill(devPassword)
  await page.getByRole("button", { name: "Set up a second factor" }).click()

  const secret = (await page.locator("p.font-mono").innerText()).trim()
  await page.locator("#totp-code").fill(totpCode(secret))
  await page.getByRole("button", { name: "Turn on the second factor" }).click()
  await expect(page.getByText("Save these recovery codes now.")).toBeVisible()
  const recovery = await page.locator("ul.font-mono li").allInnerTexts()
  expect(recovery.length).toBeGreaterThanOrEqual(2)

  // a new visitor gets the password and then the code
  await openUserMenu(page)
  await page.getByRole("menuitem", { name: "Sign out" }).click()
  await signIn(page)
  await expect(page.getByLabel("Authentication code")).toBeVisible()
  await page.getByLabel("Authentication code").fill("000000")
  await page.getByRole("button", { name: "Verify" }).click()
  await expect(page.getByText("That code is not right, or it has expired.")).toBeVisible()

  await signIn(page)
  await page.getByLabel("Authentication code").fill(recovery[0]!.trim())
  await page.getByRole("button", { name: "Verify" }).click()
  // the panel sends the visitor back to the page that asked for the session: the account
  await expect(page.getByText("Signed in as dev")).toBeVisible()

  // turn it off with the password and the second recovery code
  await page.locator("#totp-off-password").fill(devPassword)
  await page.locator("#totp-off-code").fill(recovery[1]!.trim())
  await page.getByRole("button", { name: "Turn off the second factor" }).click()
  await expect(page.getByRole("button", { name: "Set up a second factor" })).toBeVisible()
})

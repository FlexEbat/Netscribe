import { appendFileSync } from "node:fs"
import { expect, test } from "@playwright/test"
import { devPassword, openUserMenu, signIn, signInAndWait, totpCode } from "./helpers"

// The file name sorts last on purpose: with one worker the files run in name order, and if this
// test fails halfway the account keeps its second factor, which must not break the other specs.
// The whole life of a second factor on the account "dev": turn it on, sign in with a
// recovery code, turn it off. The account ends the way it began, so the other specs do not
// notice it.
// scripts/e2e.sh sets E2E_TIMINGS and prints this file as a CI notice, so a slow step of this
// spec can be told from a slow request (amendment 54, open item 4). Without the variable
// nothing is written.
const timingsFile = process.env.E2E_TIMINGS
const began = Date.now()
function note(what: string) {
  if (timingsFile) appendFileSync(timingsFile, `+${Date.now() - began}ms ${what}\n`)
}

test("the second factor can be turned on, asked at sign in and turned off", async ({ page }) => {
  page.on("response", (r) => {
    const path = new URL(r.url()).pathname
    if (path.startsWith("/api/auth")) note(`${r.request().method()} ${path} ${r.status()}`)
  })
  page.on("requestfailed", (r) => note(`request failed ${r.url()}`))
  // A wrong code and a recovery code are each checked against up to ten slow password hashes
  // (all of the unused recovery codes), which a shared CI runner can take many seconds for.
  test.setTimeout(120_000)
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
  note("second factor is on")
  await openUserMenu(page)
  await page.getByRole("menuitem", { name: "Sign out" }).click()
  await signIn(page)
  await expect(page.getByLabel("Authentication code")).toBeVisible()
  note("the code box is shown")
  await page.getByLabel("Authentication code").fill("000000")
  await page.getByRole("button", { name: "Verify" }).click()
  await expect(page.getByText("That code is not right, or it has expired.")).toBeVisible({
    timeout: 45_000,
  })
  note("the wrong code is refused")

  await signIn(page)
  await page.getByLabel("Authentication code").fill(recovery[0]!.trim())
  note("the recovery code is typed")
  await page.getByRole("button", { name: "Verify" }).click()
  // the panel sends the visitor back to the page that asked for the session: the account
  await expect(page.getByText("Signed in as dev")).toBeVisible({ timeout: 45_000 })
  note("signed in with the recovery code")

  // turn it off with the password and the second recovery code
  await page.locator("#totp-off-password").fill(devPassword)
  await page.locator("#totp-off-code").fill(recovery[1]!.trim())
  await page.getByRole("button", { name: "Turn off the second factor" }).click()
  await expect(page.getByRole("button", { name: "Set up a second factor" })).toBeVisible()
})

import { expect, test } from "@playwright/test"
import { devPassword, devUser } from "./helpers"

// A scan of the lab network of deploy/compose.lab.yaml, started from the browser (make
// e2e-lab): the three containers reach the inventory, and the scan leaves changes behind.

const labIPs = ["172.30.0.10", "172.30.0.11", "172.30.0.12"]

test("a scan of the lab network ends with its devices in the inventory and in the feed", async ({
  page,
}) => {
  test.setTimeout(180_000)
  await page.goto("/login")
  await page.getByLabel("Username").fill(devUser)
  await page.getByLabel("Password").fill(devPassword)
  await page.getByRole("button", { name: "Sign in" }).click()
  await expect(page).not.toHaveURL(/\/login/)

  const count = async () => {
    const res = await page.request.get("/api/changes")
    expect(res.status()).toBe(200)
    return ((await res.json()) as { changes: unknown[] }).changes.length
  }
  const before = await count()

  await page.getByRole("button", { name: "Run scan" }).click()

  // the scan runs in the background: look at the inventory until its devices are there
  await expect(async () => {
    await page.goto("/inventory")
    for (const ip of labIPs) {
      await expect(page.getByText(ip).first()).toBeVisible({ timeout: 3000 })
    }
  }).toPass({ timeout: 120_000 })

  expect(await count()).toBeGreaterThan(before)
})

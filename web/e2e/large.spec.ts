import { expect, test, type Page } from "@playwright/test"
import { devPassword, devUser } from "./helpers"

// These run against a generated network of hundreds of devices (make e2e-large). They check
// the rules of slice 43: the inventory does not draw every row, the topology answers a drag
// without a long task, and a bulk edit keeps its limit of 200 devices.

async function open(page: Page) {
  await page.goto("/login")
  await page.getByLabel("Username").fill(devUser)
  await page.getByLabel("Password").fill(devPassword)
  await page.getByRole("button", { name: "Sign in" }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test("the inventory draws 200 rows first and more on request", async ({ page }) => {
  await open(page)
  await page.goto("/inventory")
  await expect(page.getByRole("button", { name: /^Show 200 more/ })).toBeVisible()
  // the header row and 200 devices
  await expect(page.getByRole("row")).toHaveCount(201)
  await page.getByRole("button", { name: /^Show 200 more/ }).click()
  await expect(page.getByRole("row")).toHaveCount(401)
})

test("the topology answers a drag and shows no long task over 500 ms", async ({ page }) => {
  await open(page)
  await page.goto("/")
  await expect(page.locator(".react-flow__node").first()).toBeVisible()
  const viewport = page.locator(".react-flow__viewport")
  const before = await viewport.getAttribute("style")

  await page.evaluate(() => {
    const w = window as unknown as { __longest: number }
    w.__longest = 0
    new PerformanceObserver((list) => {
      for (const e of list.getEntries()) w.__longest = Math.max(w.__longest, e.duration)
    }).observe({ entryTypes: ["longtask"] })
  })

  const pane = page.locator(".react-flow__pane")
  const box = await pane.boundingBox()
  if (!box) throw new Error("the topology pane has no box")
  // A corner of the pane: the middle may sit on a node, and a drag on a node moves the node.
  const x = box.x + 12
  const y = box.y + box.height - 12
  await page.mouse.move(x, y)
  await page.mouse.down()
  await page.mouse.move(x + 150, y - 90, { steps: 20 })
  await page.mouse.up()

  await expect.poll(() => viewport.getAttribute("style")).not.toBe(before)
  const longest = await page.evaluate(() => (window as unknown as { __longest: number }).__longest)
  expect(longest).toBeLessThan(500)
})

test("a bulk edit of 201 devices is refused with the usual text", async ({ page }) => {
  await open(page)
  const me = await page.request.get("/api/auth/me")
  const { csrfToken } = (await me.json()) as { csrfToken: string }
  const res = await page.request.post("/api/devices/bulk", {
    headers: { "X-CSRF-Token": csrfToken },
    data: { ids: Array.from({ length: 201 }, (_, i) => i + 1), trusted: true },
  })
  expect(res.status()).toBe(400)
  expect(await res.text()).toContain("ids must hold 1 to 200 devices")
})

test("at the zoom that fits the network a node is an icon, and the text returns on zoom in", async ({
  page,
}) => {
  await open(page)
  await page.goto("/")
  const first = page.locator(".react-flow__node").first()
  await expect(first).toBeVisible()
  await expect(first).toHaveAttribute("data-lod", "far")
  expect(await first.innerText()).toBe("")

  const pane = page.locator(".react-flow__pane")
  const box = await pane.boundingBox()
  if (!box) throw new Error("the topology pane has no box")
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
  await expect
    .poll(async () => {
      await page.mouse.wheel(0, -400)
      return page.locator(".react-flow__node").filter({ hasText: "dev-" }).count()
    })
    .toBeGreaterThan(0)
})

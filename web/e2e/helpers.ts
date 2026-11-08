import { createHmac } from "node:crypto"
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
  // The panel opens on the topology. A node is drawn whatever the language and the zoom: at
  // a small width the zoom is far and a node shows its icon only, without the name.
  await expect(page.locator(".react-flow__node").first()).toBeVisible()
}

export async function openUserMenu(page: Page) {
  await page.getByRole("button", { name: new RegExp(`^${devUser}`) }).click()
}

/** The 6 digit code of RFC 6238 (SHA-1, 30 second steps) for a base32 key, as an authenticator app computes it. */
export function totpCode(secret: string, at = Date.now()): string {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
  let bits = ""
  for (const ch of secret.replace(/[\s=]/g, "").toUpperCase()) {
    bits += alphabet.indexOf(ch).toString(2).padStart(5, "0")
  }
  const key = Buffer.from(bits.match(/.{8}/g)?.map((b) => parseInt(b, 2)) ?? [])
  const counter = Buffer.alloc(8)
  counter.writeBigUInt64BE(BigInt(Math.floor(at / 1000 / 30)))
  const mac = createHmac("sha1", key).update(counter).digest()
  const offset = mac[mac.length - 1]! & 0x0f
  const value = mac.readUInt32BE(offset) & 0x7fffffff
  return String(value % 1_000_000).padStart(6, "0")
}

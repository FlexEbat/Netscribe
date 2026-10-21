import { fileURLToPath, URL } from "node:url"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig, type Plugin } from "vitest/config"

const backend = "http://127.0.0.1:8080"

// Two dependencies add their CSS with a <style> element at run time: sonner for toasts, and
// react-style-singleton for the scroll lock of dialogs and menus. The CSP (style-src 'self')
// blocks such elements and the browser reports a violation. This plugin turns that code off.
// The toast CSS is imported from sonner/dist/styles.css in components/ui/sonner.tsx and the
// scroll lock rule is in index.css. A dependency update that changes the code fails the
// build, so the CSP does not break unnoticed.
const noInlineStyleElements = (): Plugin => {
  const patches: { file: RegExp; from: string; to: string }[] = [
    {
      file: /sonner[\\/]dist[\\/]index\.mjs$/,
      from: "if (!code || typeof document == 'undefined') return",
      to: "return",
    },
    {
      file: /react-style-singleton[\\/]dist[\\/]es2015[\\/]singleton\.js$/,
      from: "if ((stylesheet = makeStyleTag())) {",
      to: "if (false) {",
    },
  ]
  return {
    name: "no-inline-style-elements",
    enforce: "pre",
    transform(code, id) {
      const patch = patches.find((p) => p.file.test(id))
      if (!patch) return null
      if (!code.includes(patch.from)) {
        this.error(
          `${id}: the style injection changed, update noInlineStyleElements in vite.config.ts`,
        )
      }
      return { code: code.replace(patch.from, patch.to), map: null }
    },
  }
}

export default defineConfig({
  plugins: [noInlineStyleElements(), react(), tailwindcss()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  server: {
    proxy: {
      "/api": backend,
      "/healthz": backend,
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test-setup.ts"],
    restoreMocks: true,
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
})

import js from "@eslint/js"
import jsxA11y from "eslint-plugin-jsx-a11y"
import react from "eslint-plugin-react"
import reactHooks from "eslint-plugin-react-hooks"
import globals from "globals"
import tseslint from "typescript-eslint"

export default tseslint.config(
  { ignores: ["dist", "node_modules", "e2e-results", "playwright-report"] },
  { settings: { react: { version: "detect" } } },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  react.configs.flat.recommended,
  react.configs.flat["jsx-runtime"],
  jsxA11y.flatConfigs.recommended,
  reactHooks.configs.flat.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: { globals: globals.browser },
    rules: {
      // Data from the network is untrusted: output goes through React escaping only.
      "react/no-danger": "error",
    },
  },
  {
    // A fixed sleep hides a race: wait on a locator or a response instead (tech.md slice 42).
    files: ["e2e/**/*.ts"],
    languageOptions: { globals: globals.node },
    rules: {
      "no-restricted-properties": [
        "error",
        { property: "waitForTimeout", message: "Wait on a locator or a response, not on a timer." },
      ],
    },
  },
  {
    files: ["public/*.js"],
    languageOptions: { globals: globals.browser },
  },
  {
    files: ["*.config.{js,ts}"],
    languageOptions: { globals: globals.node },
  },
)

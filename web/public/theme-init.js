// Sets the theme class before the first paint, so a reload does not flash the wrong theme.
// It is an external file because the CSP forbids inline scripts. The key and the rule
// match src/lib/theme.ts.
;(function () {
  var choice = null
  try {
    choice = localStorage.getItem("netscribe.theme")
  } catch {
    // Storage can be blocked. The system setting decides then.
  }
  var dark =
    choice === "dark" ||
    (choice !== "light" && window.matchMedia("(prefers-color-scheme: dark)").matches)
  document.documentElement.classList.toggle("dark", dark)
})()

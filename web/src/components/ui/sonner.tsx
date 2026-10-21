import { Toaster as Sonner, type ToasterProps } from "sonner"
import "sonner/dist/styles.css"
import { useTheme } from "@/lib/theme"

/** The app's toast stack. It follows the theme the user chose. */
function Toaster(props: ToasterProps) {
  const { resolved } = useTheme()
  return (
    <Sonner
      theme={resolved}
      className="toaster group"
      style={
        {
          "--normal-bg": "var(--popover)",
          "--normal-text": "var(--popover-foreground)",
          "--normal-border": "var(--border)",
        } as React.CSSProperties
      }
      {...props}
    />
  )
}

export { Toaster }

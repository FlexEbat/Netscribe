import { ArrowSquareOut, Copy } from "@phosphor-icons/react"
import { useState } from "react"
import type { Service } from "@/api/client"
import { Button } from "@/components/ui/button"
import { sshCommand, webUrl } from "@/lib/links"
import { t } from "@/i18n"

/**
 * What a person can do with a service: open a web page, or copy the ssh command. It shows
 * nothing for other services. The link comes only from the address and the port.
 */
export function ServiceLink({
  address,
  service,
}: {
  address: string
  service: Pick<Service, "port" | "name">
}) {
  const url = webUrl(address, service.port, service.name)
  const command = sshCommand(address, service.port, service.name)
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle")

  if (url) {
    return (
      <a
        href={url}
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex items-center gap-1 text-sm underline underline-offset-4"
      >
        {t("serviceLink.open")}
        <ArrowSquareOut className="size-4" aria-hidden="true" />
        <span className="sr-only">{t("serviceLink.inNewTab", { url })}</span>
      </a>
    )
  }
  if (command) {
    return (
      <div className="flex flex-col gap-1">
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            // navigator.clipboard exists only on HTTPS and localhost, and a LAN panel is
            // often served over plain HTTP, so a failure shows the command to select.
            const write = navigator.clipboard?.writeText(command)
            if (!write) {
              setState("failed")
              return
            }
            write.then(
              () => setState("copied"),
              () => setState("failed"),
            )
          }}
        >
          <Copy className="size-4" aria-hidden="true" />
          {t(state === "copied" ? "serviceLink.copied" : "serviceLink.copySsh")}
        </Button>
        {state === "failed" ? (
          <code className="text-sm break-all select-all">{command}</code>
        ) : null}
      </div>
    )
  }
  return null
}

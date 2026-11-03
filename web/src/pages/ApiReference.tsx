import { useEffect, useMemo, useState } from "react"
import { getApiDocument, type ApiDocument, type ApiOperation } from "@/api/client"
import { Alert } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { t } from "@/i18n"
import { tRich } from "@/i18n/rich"

type State = { status: "loading" } | { status: "failed" } | { status: "ready"; doc: ApiDocument }

interface Row {
  method: string
  path: string
  op: ApiOperation
}

const order = ["get", "post", "put", "patch", "delete"]

/** One row per operation, grouped by what the path starts with. */
function rows(doc: ApiDocument): Map<string, Row[]> {
  const groups = new Map<string, Row[]>()
  for (const [path, item] of Object.entries(doc.paths)) {
    for (const [method, op] of Object.entries(item)) {
      const key =
        path === "/metrics" || path === "/healthz" ? "service" : (path.split("/")[2] ?? "service")
      groups.set(key, [...(groups.get(key) ?? []), { method, path, op }])
    }
  }
  for (const list of groups.values()) {
    list.sort(
      (a, b) => a.path.localeCompare(b.path) || order.indexOf(a.method) - order.indexOf(b.method),
    )
  }
  return new Map([...groups.entries()].sort(([a], [b]) => a.localeCompare(b)))
}

/** The example never holds a real token: it reads one from the variable NETSCRIBE_TOKEN. */
function example(method: string, path: string): string {
  const url = `${window.location.origin}${path}`
  const verb = method === "get" ? "" : ` -X ${method.toUpperCase()}`
  return `curl${verb} -H "Authorization: Bearer $NETSCRIBE_TOKEN" ${url}`
}

/** The routes of the API with what each needs, built from the server's own route table. */
export function ApiReference() {
  const [state, setState] = useState<State>({ status: "loading" })

  useEffect(() => {
    let live = true
    getApiDocument()
      .then((doc) => live && setState({ status: "ready", doc }))
      .catch(() => live && setState({ status: "failed" }))
    return () => {
      live = false
    }
  }, [])

  const groups = useMemo(() => (state.status === "ready" ? rows(state.doc) : null), [state])

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold">{t("apiReference.apiReference")}</h1>
      <p className="text-sm text-muted-foreground">
        {tRich("apiReference.intro", {
          header: (
            <code className="font-mono text-xs">{t("apiReference.authorizationBearer")}</code>
          ),
          openapi: <code className="font-mono text-xs">{t("apiReference.apiOpenapiJson")}</code>,
        })}
      </p>
      {state.status === "loading" ? (
        <div
          role="status"
          aria-label={t("apiReference.loadingTheReference")}
          className="flex flex-col gap-2"
        >
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
        </div>
      ) : null}
      {state.status === "failed" ? (
        <Alert variant="destructive">{t("apiReference.theReferenceCouldNotBe")}</Alert>
      ) : null}
      {groups
        ? [...groups.entries()].map(([name, list]) => (
            <section key={name} className="flex flex-col gap-3">
              <h2 className="text-lg font-semibold capitalize">{name}</h2>
              <ul className="flex flex-col gap-3">
                {list.map(({ method, path, op }) => (
                  <li key={method + path} className="rounded-lg border p-4">
                    <div className="flex flex-wrap items-center gap-3">
                      <Badge variant="outline" className="font-mono uppercase">
                        {method}
                      </Badge>
                      <code className="font-mono text-sm">{path}</code>
                      <Badge variant={op["x-token"] ? "outline" : "default"}>
                        {t(op["x-token"] ? "apiReference.tokensMay" : "apiReference.notForTokens")}
                      </Badge>
                    </div>
                    <p className="mt-2 text-sm">{op.summary}</p>
                    <p className="mt-1 text-sm text-muted-foreground">{op.description}</p>
                    {op.parameters?.filter((p) => p.in === "query").length ? (
                      <ul className="mt-2 text-sm text-muted-foreground">
                        {op.parameters
                          .filter((p) => p.in === "query")
                          .map((p) => (
                            <li key={p.name}>
                              <code className="font-mono text-xs">{p.name}</code>: {p.description}
                            </li>
                          ))}
                      </ul>
                    ) : null}
                    {op.requestBody ? (
                      <p className="mt-2 text-sm text-muted-foreground">
                        {t("apiReference.body", { text: op.requestBody.description })}
                      </p>
                    ) : null}
                    {op["x-token"] ? (
                      <pre className="mt-2 overflow-x-auto rounded-md bg-muted p-2 font-mono text-xs">
                        {example(method, path)}
                      </pre>
                    ) : null}
                  </li>
                ))}
              </ul>
            </section>
          ))
        : null}
    </div>
  )
}

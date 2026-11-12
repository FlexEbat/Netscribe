import { useEffect, useState } from "react"
import { getOverview, type Overview } from "@/api/client"

const refreshEvery = 30 * 1000

/**
 * The counts of the network for the header and the menu, read again every 30 seconds. A
 * failed request keeps what was there: the readouts are a convenience.
 */
export function useOverview(enabled: boolean): Overview | null {
  const [data, setData] = useState<Overview | null>(null)

  useEffect(() => {
    if (!enabled) return
    let alive = true
    const load = () => {
      getOverview()
        .then((next) => alive && setData(next))
        .catch(() => undefined)
    }
    load()
    const timer = window.setInterval(load, refreshEvery)
    return () => {
      alive = false
      window.clearInterval(timer)
    }
  }, [enabled])

  return data
}

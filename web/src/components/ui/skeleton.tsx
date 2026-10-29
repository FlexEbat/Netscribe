import * as React from "react"
import { cn } from "@/lib/utils"

function Skeleton({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      className={cn(
        "animate-[skeleton_1.4s_cubic-bezier(0.22,1,0.36,1)_infinite_alternate] rounded-md bg-muted",
        className,
      )}
      {...props}
    />
  )
}

export { Skeleton }

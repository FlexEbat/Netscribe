import * as React from "react"
import { cn } from "@/lib/utils"

function Table({ className, ...props }: React.ComponentProps<"table">) {
  return (
    <div className="w-full overflow-x-auto">
      <table className={cn("w-full text-left text-sm", className)} {...props} />
    </div>
  )
}

function TableHeader({ className, ...props }: React.ComponentProps<"thead">) {
  return <thead className={cn("border-b text-muted-foreground", className)} {...props} />
}

function TableBody({ className, ...props }: React.ComponentProps<"tbody">) {
  return <tbody className={className} {...props} />
}

function TableRow({ className, ...props }: React.ComponentProps<"tr">) {
  return <tr className={cn("border-b last:border-0", className)} {...props} />
}

function TableHead({ className, ...props }: React.ComponentProps<"th">) {
  return <th scope="col" className={cn("py-2 pr-4 font-medium", className)} {...props} />
}

function TableCell({ className, ...props }: React.ComponentProps<"td">) {
  return <td className={cn("py-2 pr-4", className)} {...props} />
}

function TableCaption({ className, ...props }: React.ComponentProps<"caption">) {
  return <caption className={cn("mb-3 text-left text-lg font-semibold", className)} {...props} />
}

export { Table, TableBody, TableCaption, TableCell, TableHead, TableHeader, TableRow }

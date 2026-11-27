import React from "react"
import { cn } from "@/lib/utils"

interface ResponsiveTableProps {
  id: string
  caption: string
  headers: string[]
  children: React.ReactNode
  className?: string
  empty?: boolean
  emptyMessage?: string
}

export function ResponsiveTable({
  id,
  caption,
  headers,
  children,
  className,
  empty,
  emptyMessage = "No records found.",
}: ResponsiveTableProps) {
  if (empty) {
    return (
      <div className="border border-dashed border-border bg-card/50 p-8 text-center text-xs text-muted-foreground">
        {emptyMessage}
      </div>
    )
  }

  return (
    <div
      tabIndex={0}
      role="region"
      aria-labelledby={id}
      className={cn("runway-table-region border border-border bg-card focus-visible:outline-2 focus-visible:outline-ring", className)}
    >
      <table className="runway-table">
        <caption id={id} className="sr-only">
          {caption}
        </caption>
        <thead>
          <tr>
            {headers.map((h) => (
              <th key={h} scope="col">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>{children}</tbody>
      </table>
    </div>
  )
}

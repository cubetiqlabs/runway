import React, { useEffect, useId, useMemo, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  ArrowUp01Icon,
  ArrowLeft01Icon,
  ArrowRight01Icon,
  Search01Icon,
  Settings02Icon,
  Cancel01Icon,
} from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"

export type SortDirection = "asc" | "desc"

export interface DataTableColumn<T> {
  id: string
  header: string
  accessorKey?: keyof T & string
  sortKey?: string
  sortable?: boolean
  sortType?: "string" | "number"
  sortValue?: (row: T) => string | number
  cell: (row: T) => React.ReactNode
  isRowHeader?: boolean
  defaultVisible?: boolean
  hideable?: boolean
  headerClassName?: string
  cellClassName?: string
}

export interface DataTableFilterOption {
  label: string
  value: string
}

export interface DataTableFilter {
  id: string
  label: string
  value: string
  onChange: (value: string) => void
  options: DataTableFilterOption[]
  className?: string
}

export interface DataTablePagination {
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number) => void
  onPageSizeChange?: (pageSize: number) => void
  pageSizeOptions?: number[]
}

export interface DataTableProps<T> {
  id: string
  caption: string
  data: T[]
  columns: DataTableColumn<T>[]
  getRowId: (row: T) => string
  loading?: boolean
  emptyMessage?: string
  searchValue?: string
  onSearchChange?: (value: string) => void
  searchPlaceholder?: string
  filters?: DataTableFilter[]
  onResetFilters?: () => void
  sortBy?: string
  sortDir?: SortDirection
  onSortChange?: (sortBy: string, sortDir: SortDirection) => void
  pagination?: DataTablePagination
  toolbarActions?: React.ReactNode
  onRowClick?: (row: T) => void
  className?: string
}

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" })

function loadColumnVisibility(storageKey: string, columns: DataTableColumn<any>[]): Record<string, boolean> {
  const defaults: Record<string, boolean> = {}
  for (const c of columns) {
    defaults[c.id] = c.defaultVisible !== false
  }
  if (typeof window === "undefined") {
    return defaults
  }
  try {
    const raw = window.localStorage.getItem(storageKey)
    if (!raw) return defaults
    const parsed = JSON.parse(raw) as Record<string, boolean>
    return { ...defaults, ...parsed }
  } catch {
    return defaults
  }
}

export function DataTable<T>({
  id,
  caption,
  data,
  columns,
  getRowId,
  loading = false,
  emptyMessage = "No records found.",
  searchValue,
  onSearchChange,
  searchPlaceholder = "Search...",
  filters,
  onResetFilters,
  sortBy: controlledSortBy,
  sortDir: controlledSortDir,
  onSortChange,
  pagination,
  toolbarActions,
  onRowClick,
  className,
}: DataTableProps<T>) {
  const captionId = useId()
  const searchInputId = useId()
  const storageKey = `runway:dt:cols:${id}`

  const [visibleMap, setVisibleMap] = useState<Record<string, boolean>>(() =>
    loadColumnVisibility(storageKey, columns)
  )
  const [localSortBy, setLocalSortBy] = useState<string>("")
  const [localSortDir, setLocalSortDir] = useState<SortDirection>("asc")
  const [announcement, setAnnouncement] = useState("")
  const [localSearch, setLocalSearch] = useState(searchValue ?? "")

  useEffect(() => {
    if (searchValue !== undefined) {
      setLocalSearch(searchValue)
    }
  }, [searchValue])

  useEffect(() => {
    if (!onSearchChange) return
    const timer = window.setTimeout(() => {
      if (localSearch !== (searchValue ?? "")) {
        onSearchChange(localSearch)
      }
    }, 250)
    return () => window.clearTimeout(timer)
  }, [localSearch, onSearchChange, searchValue])

  useEffect(() => {
    if (typeof window === "undefined") return
    try {
      window.localStorage.setItem(storageKey, JSON.stringify(visibleMap))
    } catch {
      // ignore storage quota errors
    }
  }, [storageKey, visibleMap])

  const activeSortBy = controlledSortBy !== undefined ? controlledSortBy : localSortBy
  const activeSortDir = controlledSortDir !== undefined ? controlledSortDir : localSortDir

  const visibleColumns = useMemo(
    () => columns.filter((col) => visibleMap[col.id] !== false),
    [columns, visibleMap]
  )

  const sortedRows = useMemo(() => {
    if (onSortChange || !activeSortBy) {
      return data
    }
    const col = columns.find((c) => (c.sortKey || c.id) === activeSortBy)
    if (!col) return data

    const dirFactor = activeSortDir === "asc" ? 1 : -1
    const getVal = (row: T): string | number => {
      if (col.sortValue) return col.sortValue(row)
      if (col.accessorKey) {
        const v = (row as Record<string, unknown>)[col.accessorKey]
        if (typeof v === "number") return v
        if (v === null || v === undefined) return ""
        return String(v)
      }
      return ""
    }

    return [...data].sort((a, b) => {
      const va = getVal(a)
      const vb = getVal(b)
      if (col.sortType === "number" || (typeof va === "number" && typeof vb === "number")) {
        return dirFactor * (Number(va) - Number(vb))
      }
      return dirFactor * collator.compare(String(va), String(vb))
    })
  }, [data, columns, activeSortBy, activeSortDir, onSortChange])

  const handleSort = (col: DataTableColumn<T>) => {
    if (!col.sortable) return
    const key = col.sortKey || col.id
    const nextDir: SortDirection =
      activeSortBy === key && activeSortDir === "asc" ? "desc" : "asc"

    if (onSortChange) {
      onSortChange(key, nextDir)
    } else {
      setLocalSortBy(key)
      setLocalSortDir(nextDir)
    }
    setAnnouncement(
      `Sorted by ${col.header}, ${nextDir === "asc" ? "ascending" : "descending"}, ${sortedRows.length} rows`
    )
  }

  const toggleColumn = (colId: string, checked: boolean) => {
    setVisibleMap((prev) => {
      const next = { ...prev, [colId]: checked }
      const remaining = columns.filter((c) => next[c.id] !== false).length
      if (remaining === 0) return prev
      return next
    })
  }

  const resetColumns = () => {
    const defaults: Record<string, boolean> = {}
    for (const c of columns) {
      defaults[c.id] = c.defaultVisible !== false
    }
    setVisibleMap(defaults)
  }

  const hasActiveFilters =
    Boolean(localSearch.trim()) ||
    Boolean(filters?.some((f) => f.value !== "" && f.value !== "all"))

  const pageSizeOptions = pagination?.pageSizeOptions ?? [10, 25, 50, 100]
  const total = pagination?.total ?? sortedRows.length
  const pageSize = pagination?.pageSize ?? 25
  const page = pagination?.page ?? 1
  const totalPages = Math.max(1, Math.ceil(total / Math.max(1, pageSize)))
  const startIdx = total === 0 ? 0 : (page - 1) * pageSize + 1
  const endIdx = total === 0 ? 0 : Math.min(total, (page - 1) * pageSize + sortedRows.length)

  return (
    <div className={cn("space-y-3", className)}>
      {(onSearchChange || (filters && filters.length > 0) || columns.some((c) => c.hideable !== false) || toolbarActions) && (
        <div className="flex flex-wrap items-center justify-between gap-2.5">
          <div className="flex flex-wrap items-center gap-2 flex-1 min-w-0">
            {onSearchChange && (
              <search className="relative flex-1 min-w-44 max-w-xs">
                <label htmlFor={searchInputId} className="sr-only">
                  {searchPlaceholder}
                </label>
                <HugeiconsIcon
                  icon={Search01Icon}
                  strokeWidth={2}
                  className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground"
                />
                <Input
                  id={searchInputId}
                  type="search"
                  value={localSearch}
                  onChange={(e) => setLocalSearch(e.target.value)}
                  placeholder={searchPlaceholder}
                  className="pl-8 pr-7 h-8 text-xs"
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                />
                {localSearch && (
                  <button
                    type="button"
                    onClick={() => {
                      setLocalSearch("")
                      onSearchChange("")
                    }}
                    aria-label="Clear search"
                    className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  >
                    <HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} className="size-3.5" />
                  </button>
                )}
              </search>
            )}

            {filters?.map((f) => (
              <div key={f.id} className={cn("min-w-36", f.className)}>
                <Select value={f.value} onValueChange={(v) => f.onChange(v || "all")}>
                  <SelectTrigger aria-label={f.label} className="h-8 text-xs w-full">
                    <SelectValue placeholder={f.label} />
                  </SelectTrigger>
                  <SelectContent>
                    {f.options.map((opt) => (
                      <SelectItem key={opt.value} value={opt.value}>
                        {opt.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            ))}

            {hasActiveFilters && onResetFilters && (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-8 text-xs"
                onClick={() => {
                  setLocalSearch("")
                  onResetFilters()
                }}
              >
                Reset
              </Button>
            )}
          </div>

          <div className="flex flex-wrap items-center gap-2">
            {toolbarActions}

            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button type="button" variant="outline" size="sm" className="h-8 text-xs">
                    <HugeiconsIcon icon={Settings02Icon} strokeWidth={2} className="size-3.5" />
                    <span>Columns</span>
                  </Button>
                }
              />
              <DropdownMenuContent align="end" className="w-48">
                <DropdownMenuLabel>Visible columns</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {columns
                  .filter((c) => c.hideable !== false)
                  .map((col) => (
                    <DropdownMenuCheckboxItem
                      key={col.id}
                      checked={visibleMap[col.id] !== false}
                      onCheckedChange={(checked) => toggleColumn(col.id, Boolean(checked))}
                    >
                      {col.header}
                    </DropdownMenuCheckboxItem>
                  ))}
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={resetColumns}>Reset columns</DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      )}

      <div role="status" className="sr-only">
        {announcement}
      </div>

      {loading && sortedRows.length === 0 ? (
        <div className="space-y-2 border border-border bg-card p-4">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-9 w-full" />
          ))}
        </div>
      ) : !loading && sortedRows.length === 0 ? (
        <div className="border border-dashed border-border bg-card/50 p-8 text-center text-xs text-muted-foreground">
          {emptyMessage}
        </div>
      ) : (
        <div
          tabIndex={0}
          role="region"
          aria-labelledby={captionId}
          className="runway-table-region border border-border bg-card focus-visible:outline-2 focus-visible:outline-ring"
        >
          <table className="runway-table">
            <caption id={captionId} className="sr-only">
              {caption}
            </caption>
            <thead>
              <tr>
                {visibleColumns.map((col) => {
                  const key = col.sortKey || col.id
                  const isSorted = col.sortable && activeSortBy === key
                  const ariaSort = isSorted
                    ? activeSortDir === "asc"
                      ? "ascending"
                      : "descending"
                    : undefined

                  return (
                    <th
                      key={col.id}
                      scope="col"
                      aria-sort={ariaSort}
                      className={col.headerClassName}
                    >
                      {col.sortable ? (
                        <button
                          type="button"
                          className="runway-table-sort-btn"
                          onClick={() => handleSort(col)}
                        >
                          <span>{col.header}</span>
                          <HugeiconsIcon
                            icon={ArrowUp01Icon}
                            strokeWidth={2}
                            aria-hidden="true"
                            className="runway-sort-icon size-3.5 shrink-0"
                          />
                        </button>
                      ) : (
                        col.header
                      )}
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody>
              {sortedRows.map((row) => (
                <tr
                  key={getRowId(row)}
                  onClick={onRowClick ? () => onRowClick(row) : undefined}
                  className={onRowClick ? "cursor-pointer" : undefined}
                >
                  {visibleColumns.map((col, idx) => {
                    const isHeader = col.isRowHeader || (idx === 0 && !visibleColumns.some((c) => c.isRowHeader))
                    if (isHeader) {
                      return (
                        <th key={col.id} scope="row" className={col.cellClassName}>
                          {col.cell(row)}
                        </th>
                      )
                    }
                    return (
                      <td key={col.id} data-label={col.header} className={col.cellClassName}>
                        {col.cell(row)}
                      </td>
                    )
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {pagination && (
        <div className="flex flex-wrap items-center justify-between gap-3 border border-border bg-card px-3 py-2 text-xs">
          <div className="text-muted-foreground">
            Showing <span className="font-medium text-foreground">{startIdx}</span>–
            <span className="font-medium text-foreground">{endIdx}</span> of{" "}
            <span className="font-medium text-foreground">{total}</span>
          </div>

          <div className="flex flex-wrap items-center gap-3">
            {pagination.onPageSizeChange && (
              <div className="flex items-center gap-1.5">
                <span className="text-muted-foreground hidden sm:inline">Rows</span>
                <Select
                  value={String(pageSize)}
                  onValueChange={(v) => {
                    const n = Number(v)
                    if (n > 0) pagination.onPageSizeChange?.(n)
                  }}
                >
                  <SelectTrigger aria-label="Rows per page" className="h-7 w-16 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {pageSizeOptions.map((n) => (
                      <SelectItem key={n} value={String(n)}>
                        {n}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}

            <div className="flex items-center gap-1">
              <Button
                type="button"
                variant="outline"
                size="xs"
                disabled={page <= 1 || loading}
                onClick={() => pagination.onPageChange(1)}
                aria-label="First page"
              >
                First
              </Button>
              <Button
                type="button"
                variant="outline"
                size="xs"
                disabled={page <= 1 || loading}
                onClick={() => pagination.onPageChange(page - 1)}
                aria-label="Previous page"
              >
                <HugeiconsIcon icon={ArrowLeft01Icon} strokeWidth={2} className="size-3.5" />
              </Button>
              <span className="px-2 font-mono text-[11px] text-muted-foreground whitespace-nowrap">
                {page} / {totalPages}
              </span>
              <Button
                type="button"
                variant="outline"
                size="xs"
                disabled={page >= totalPages || loading}
                onClick={() => pagination.onPageChange(page + 1)}
                aria-label="Next page"
              >
                <HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={2} className="size-3.5" />
              </Button>
              <Button
                type="button"
                variant="outline"
                size="xs"
                disabled={page >= totalPages || loading}
                onClick={() => pagination.onPageChange(totalPages)}
                aria-label="Last page"
              >
                Last
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

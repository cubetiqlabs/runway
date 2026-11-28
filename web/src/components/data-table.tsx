import React, { useEffect, useId, useMemo, useState } from "react"
import {
  columnVisibilityFeature,
  constructSortFn,
  createSortedRowModel,
  rowSortingFeature,
  sortFn_basic,
  tableFeatures,
  useTable,
  type ColumnDef,
  type ColumnVisibilityState,
  type RowData,
  type SortingState,
} from "@tanstack/react-table"
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
import {
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Skeleton } from "@/components/ui/skeleton"
import { SearchableCombobox } from "@/components/searchable-combobox"
import {
  clearTableColumnVisibility,
  setTableColumnVisibility,
  useAppSelector,
} from "@/lib/store"
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
  searchable?: boolean
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

export interface DataTableProps<T extends object> {
  id: string
  caption: string
  data: T[]
  columns: DataTableColumn<T>[]
  getRowId: (row: T) => string
  loading?: boolean
  fetching?: boolean
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
  hideColumnPicker?: boolean
  className?: string
}

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" })

const collatorSortFn = constructSortFn({
  sort: (a: unknown, b: unknown) => collator.compare(String(a ?? ""), String(b ?? "")),
})

const dtFeatures = tableFeatures({
  columnVisibilityFeature,
  rowSortingFeature,
  sortedRowModel: createSortedRowModel(),
})

export function DataTable<T extends object>({
  id,
  caption,
  data,
  columns,
  getRowId,
  loading = false,
  fetching = false,
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
  hideColumnPicker = false,
  className,
}: DataTableProps<T>) {
  const captionId = useId()
  const searchInputId = useId()

  const storedVisibility = useAppSelector((s) => s.tableColumnVisibility[id])
  const [localSorting, setLocalSorting] = useState<SortingState>([])
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
    }, 220)
    return () => window.clearTimeout(timer)
  }, [localSearch, onSearchChange, searchValue])

  const defaultVisibility = useMemo<ColumnVisibilityState>(() => {
    const out: ColumnVisibilityState = {}
    for (const c of columns) {
      out[c.id] = c.defaultVisible !== false
    }
    return out
  }, [columns])

  const columnVisibility = useMemo<ColumnVisibilityState>(
    () => ({ ...defaultVisibility, ...storedVisibility }),
    [defaultVisibility, storedVisibility]
  )

  const sortingState = useMemo<SortingState>(() => {
    if (onSortChange) {
      if (!controlledSortBy) return []
      return [{ id: controlledSortBy, desc: controlledSortDir === "desc" }]
    }
    return localSorting
  }, [onSortChange, controlledSortBy, controlledSortDir, localSorting])

  const columnMetaById = useMemo(() => {
    const map = new Map<string, DataTableColumn<T>>()
    for (const col of columns) {
      map.set(col.id, col)
    }
    return map
  }, [columns])

  type TableRowItem = T & RowData

  const columnDefs = useMemo<ColumnDef<typeof dtFeatures, TableRowItem, unknown>[]>(() => {
    return columns.map((col) => ({
      id: col.id,
      header: () => col.header,
      accessorFn: (row: TableRowItem) => {
        if (col.sortValue) return col.sortValue(row)
        if (col.accessorKey) return row[col.accessorKey]
        return ""
      },
      cell: (ctx) => col.cell(ctx.row.original),
      enableHiding: col.hideable !== false,
      enableSorting: Boolean(col.sortable),
      sortingFn: col.sortType === "number" ? sortFn_basic : collatorSortFn,
    }))
  }, [columns])

  const table = useTable<typeof dtFeatures, TableRowItem>({
    features: dtFeatures,
    data: data as TableRowItem[],
    columns: columnDefs,
    getRowId: (row) => getRowId(row),
    manualSorting: Boolean(onSortChange),
    enableSortingRemoval: false,
    state: {
      columnVisibility,
      sorting: sortingState,
    },
    onColumnVisibilityChange: (updater) => {
      const next = typeof updater === "function" ? updater(columnVisibility) : updater
      const remaining = columns.filter((c) => next[c.id] !== false).length
      if (remaining === 0) return
      setTableColumnVisibility(id, next)
    },
    onSortingChange: (updater) => {
      if (onSortChange) return
      const next = typeof updater === "function" ? updater(localSorting) : updater
      setLocalSorting(next)
    },
  })

  const visibleColumns = table.getVisibleLeafColumns()
  const rows = onSortChange ? table.getCoreRowModel().rows : table.getSortedRowModel().rows

  const activeSortBy =
    onSortChange && controlledSortBy !== undefined
      ? controlledSortBy
      : localSorting[0]?.id ?? ""
  const activeSortDir: SortDirection =
    onSortChange && controlledSortDir !== undefined
      ? controlledSortDir
      : localSorting[0]?.desc
        ? "desc"
        : "asc"

  const handleSort = (col: DataTableColumn<T>) => {
    if (!col.sortable) return
    const key = col.sortKey || col.id
    const nextDir: SortDirection =
      activeSortBy === key && activeSortDir === "asc" ? "desc" : "asc"

    if (onSortChange) {
      onSortChange(key, nextDir)
    } else {
      setLocalSorting([{ id: col.id, desc: nextDir === "desc" }])
    }
    setAnnouncement(
      `Sorted by ${col.header}, ${nextDir === "asc" ? "ascending" : "descending"}, ${rows.length} rows`
    )
  }

  const hasActiveFilters =
    Boolean(localSearch.trim()) ||
    Boolean(filters?.some((f) => f.value !== "" && f.value !== "all"))

  const pageSizeOptions = pagination?.pageSizeOptions ?? [10, 25, 50, 100]
  const total = pagination?.total ?? rows.length
  const pageSize = pagination?.pageSize ?? 25
  const page = pagination?.page ?? 1
  const totalPages = Math.max(1, Math.ceil(total / Math.max(1, pageSize)))
  const startIdx = total === 0 ? 0 : (page - 1) * pageSize + 1
  const endIdx = total === 0 ? 0 : Math.min(total, (page - 1) * pageSize + rows.length)

  const showToolbar =
    Boolean(onSearchChange) ||
    Boolean(filters && filters.length > 0) ||
    (!hideColumnPicker && columns.some((c) => c.hideable !== false)) ||
    Boolean(toolbarActions)

  return (
    <div className={cn("space-y-3", className)}>
      {showToolbar && (
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
              <div key={f.id} className={cn("min-w-40", f.className)}>
                {f.searchable ? (
                  <SearchableCombobox
                    ariaLabel={f.label}
                    options={f.options}
                    value={f.value}
                    onValueChange={(v) => f.onChange(v || "all")}
                    placeholder={f.label}
                    emptyMessage={`No matching ${f.label.toLowerCase()}`}
                    className="h-8 text-xs"
                  />
                ) : (
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
                )}
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

            {!hideColumnPicker && columns.some((c) => c.hideable !== false) && (
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
                  {table
                    .getAllLeafColumns()
                    .filter((col) => col.getCanHide())
                    .map((col) => {
                      const def = columnMetaById.get(col.id)
                      return (
                        <DropdownMenuCheckboxItem
                          key={col.id}
                          checked={col.getIsVisible()}
                          onCheckedChange={(checked) => col.toggleVisibility(Boolean(checked))}
                        >
                          {def?.header ?? col.id}
                        </DropdownMenuCheckboxItem>
                      )
                    })}
                  <DropdownMenuSeparator />
                  <DropdownMenuItem onClick={() => clearTableColumnVisibility(id)}>
                    Reset columns
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            )}
          </div>
        </div>
      )}

      <div role="status" className="sr-only">
        {announcement}
      </div>

      {loading && rows.length === 0 ? (
        <div className="space-y-2 border border-border bg-card p-4">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-9 w-full" />
          ))}
        </div>
      ) : !loading && rows.length === 0 ? (
        <div className="border border-dashed border-border bg-card/50 p-8 text-center text-xs text-muted-foreground">
          {emptyMessage}
        </div>
      ) : (
        <div
          tabIndex={0}
          role="region"
          aria-labelledby={captionId}
          className="relative runway-table-region border border-border bg-card focus-visible:outline-2 focus-visible:outline-ring"
        >
          {fetching && (
            <div
              aria-hidden="true"
              className="pointer-events-none absolute inset-x-0 top-0 z-10 h-0.5 bg-primary/80 animate-pulse"
            />
          )}
          <table className="runway-table">
            <caption id={captionId} className="sr-only">
              {caption}
            </caption>
            <TableHeader className="[&_tr]:border-b-0">
              {table.getHeaderGroups().map((headerGroup) => (
                <TableRow key={headerGroup.id} className="border-b-0 hover:bg-transparent">
                  {headerGroup.headers.map((header) => {
                    const colDef = columnMetaById.get(header.column.id)
                    if (!colDef) return null
                    const key = colDef.sortKey || colDef.id
                    const isSorted = colDef.sortable && activeSortBy === key
                    const ariaSort = isSorted
                      ? activeSortDir === "asc"
                        ? "ascending"
                        : "descending"
                      : undefined

                    return (
                      <TableHead
                        key={header.id}
                        scope="col"
                        aria-sort={ariaSort}
                        className={colDef.headerClassName}
                      >
                        {colDef.sortable ? (
                          <button
                            type="button"
                            className="runway-table-sort-btn"
                            onClick={() => handleSort(colDef)}
                          >
                            <span>{colDef.header}</span>
                            <HugeiconsIcon
                              icon={ArrowUp01Icon}
                              strokeWidth={2}
                              aria-hidden="true"
                              className="runway-sort-icon size-3.5 shrink-0"
                            />
                          </button>
                        ) : (
                          colDef.header
                        )}
                      </TableHead>
                    )
                  })}
                </TableRow>
              ))}
            </TableHeader>
            <TableBody>
              {rows.map((row) => (
                <TableRow
                  key={row.id}
                  onClick={onRowClick ? () => onRowClick(row.original) : undefined}
                  className={cn("border-b-0", onRowClick && "cursor-pointer")}
                >
                  {row.getVisibleCells().map((cell, idx) => {
                    const colDef = columnMetaById.get(cell.column.id)
                    if (!colDef) return null
                    const isHeader =
                      colDef.isRowHeader ||
                      (idx === 0 && !visibleColumns.some((c) => columnMetaById.get(c.id)?.isRowHeader))
                    if (isHeader) {
                      return (
                        <th key={cell.id} scope="row" className={colDef.cellClassName}>
                          <table.FlexRender cell={cell} />
                        </th>
                      )
                    }
                    return (
                      <TableCell
                        key={cell.id}
                        data-label={colDef.header}
                        className={colDef.cellClassName}
                      >
                        <table.FlexRender cell={cell} />
                      </TableCell>
                    )
                  })}
                </TableRow>
              ))}
            </TableBody>
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

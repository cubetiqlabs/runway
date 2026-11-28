import React, { useMemo } from "react"
import { areaY, barY, defineChart, lineY, type ChartTheme } from "@tanstack/charts"
import { Chart } from "@tanstack/charts/react"
import { tooltip } from "@tanstack/charts/tooltip"
import { scaleBand } from "@tanstack/charts/scales/band"
import { scaleLinear } from "@tanstack/charts/scales/linear"
import { scalePoint } from "@tanstack/charts/scales/point"
import { cn } from "@/lib/utils"

export interface ChartConfig {
  [key: string]: {
    label: string
    color?: string
  }
}

export const RUNWAY_CHART_THEME: Partial<ChartTheme> = {
  foreground: "var(--muted-foreground)",
  muted: "var(--muted-foreground)",
  grid: "var(--border)",
  background: "var(--card)",
  palette: [
    "var(--chart-3)",
    "var(--chart-2)",
    "var(--chart-1)",
    "var(--chart-4)",
    "var(--chart-5)",
  ],
}

class ChartErrorBoundary extends React.Component<
  { height: number; message: string; className?: string; children: React.ReactNode },
  { hasError: boolean }
> {
  state = { hasError: false }

  static getDerivedStateFromError() {
    return { hasError: true }
  }

  componentDidUpdate(prevProps: { children: React.ReactNode }) {
    if (this.state.hasError && prevProps.children !== this.props.children) {
      this.setState({ hasError: false })
    }
  }

  render() {
    if (this.state.hasError) {
      return (
        <div
          style={{ height: this.props.height }}
          className={cn(
            "flex items-center justify-center border border-dashed border-border text-[11px] text-muted-foreground",
            this.props.className
          )}
        >
          {this.props.message}
        </div>
      )
    }
    return this.props.children
  }
}

export function ChartContainer({
  config,
  className,
  children,
}: {
  config?: ChartConfig
  className?: string
  children: React.ReactNode
}) {
  const styleVars = useMemo(() => {
    const vars: Record<string, string> = {}
    if (config) {
      for (const [key, item] of Object.entries(config)) {
        if (item.color) {
          vars[`--color-${key}`] = item.color
        }
      }
    }
    return vars as React.CSSProperties
  }, [config])

  return (
    <div
      data-slot="chart"
      style={styleVars}
      className={cn("w-full min-w-0 text-xs select-none", className)}
    >
      {children}
    </div>
  )
}

export interface TimeSeriesPoint {
  label: string
  value: number
}

export function AreaLineMetricChart({
  data,
  ariaLabel,
  color = "var(--chart-3)",
  height = 140,
  yLabel,
  emptyMessage = "Waiting for telemetry samples",
  className,
}: {
  data: readonly TimeSeriesPoint[]
  ariaLabel: string
  color?: string
  height?: number
  yLabel?: string
  emptyMessage?: string
  className?: string
}) {
  const definition = useMemo(() => {
    if (data.length === 0) return null
    const indexed = data.map((pt, idx) => ({
      idx,
      label: pt.label || `#${idx + 1}`,
      value: Number.isFinite(pt.value) ? pt.value : 0,
    }))
    return defineChart(
      {
        theme: RUNWAY_CHART_THEME,
        margin: { top: 8, right: 10, bottom: 20, left: 32 },
        marks: [
          areaY(indexed, {
            x: "idx",
            y: "value",
            fill: color,
            fillOpacity: 0.16,
          }),
          lineY(indexed, {
            x: "idx",
            y: "value",
            stroke: color,
            strokeWidth: 2,
            points: indexed.length <= 24,
          }),
        ],
        scales: {
          x: {
            scale: () => scalePoint<number>().padding(0.15),
            axis: {
              line: false,
              ticks: {
                line: false,
                format: (v: number) => indexed[Number(v)]?.label ?? String(v),
              },
              tickLabels: { thin: true, fontSize: 10 },
            },
          },
          y: {
            scale: scaleLinear,
            nice: true,
            grid: { stroke: "var(--border)", strokeOpacity: 0.6 },
            axis: {
              line: false,
              ticks: false,
              label: yLabel,
              tickLabels: { fontSize: 10 },
            },
          },
        },
      },
      { tooltip }
    )
  }, [data, color, yLabel])

  if (!definition) {
    return (
      <div
        style={{ height }}
        className={cn(
          "flex items-center justify-center border border-dashed border-border text-[11px] text-muted-foreground",
          className
        )}
      >
        {emptyMessage}
      </div>
    )
  }

  return (
    <ChartErrorBoundary height={height} message={emptyMessage} className={className}>
      <ChartContainer className={className}>
        <Chart definition={definition} height={height} ariaLabel={ariaLabel} />
      </ChartContainer>
    </ChartErrorBoundary>
  )
}

export interface CategoryBarDatum {
  category: string
  value: number
}

export function CategoryBarChart({
  data,
  ariaLabel,
  color = "var(--chart-2)",
  height = 190,
  yLabel,
  emptyMessage = "No chart data available",
  className,
}: {
  data: readonly CategoryBarDatum[]
  ariaLabel: string
  color?: string
  height?: number
  yLabel?: string
  emptyMessage?: string
  className?: string
}) {
  const definition = useMemo(() => {
    if (data.length === 0) return null
    const seen = new Map<string, number>()
    const safeData = data.map((d) => {
      const base = d.category || "—"
      const count = (seen.get(base) ?? 0) + 1
      seen.set(base, count)
      return {
        category: count > 1 ? `${base} (${count})` : base,
        value: Number.isFinite(d.value) ? d.value : 0,
      }
    })
    return defineChart(
      {
        theme: RUNWAY_CHART_THEME,
        margin: { top: 8, right: 10, bottom: 26, left: 36 },
        marks: [
          barY(safeData, {
            x: "category",
            y: "value",
            fill: color,
            fillOpacity: 0.85,
            maxThickness: 38,
            radius: [2, 2, 0, 0],
          }),
        ],
        scales: {
          x: {
            scale: () => scaleBand<string>().padding(0.28),
            axis: {
              line: { stroke: "var(--border)" },
              ticks: false,
              tickLabels: { thin: true, fontSize: 10 },
            },
          },
          y: {
            scale: scaleLinear,
            nice: true,
            grid: { stroke: "var(--border)", strokeOpacity: 0.6 },
            axis: {
              line: false,
              ticks: false,
              label: yLabel,
              tickLabels: { fontSize: 10 },
            },
          },
        },
      },
      { tooltip }
    )
  }, [data, color, yLabel])

  if (!definition) {
    return (
      <div
        style={{ height }}
        className={cn(
          "flex items-center justify-center border border-dashed border-border text-[11px] text-muted-foreground",
          className
        )}
      >
        {emptyMessage}
      </div>
    )
  }

  return (
    <ChartErrorBoundary height={height} message={emptyMessage} className={className}>
      <ChartContainer className={className}>
        <Chart definition={definition} height={height} ariaLabel={ariaLabel} />
      </ChartContainer>
    </ChartErrorBoundary>
  )
}

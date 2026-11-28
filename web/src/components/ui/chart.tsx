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
    return defineChart(
      {
        theme: RUNWAY_CHART_THEME,
        margin: { top: 8, right: 10, bottom: 20, left: 32 },
        marks: [
          areaY(data, {
            x: "label",
            y: "value",
            fill: color,
            fillOpacity: 0.16,
          }),
          lineY(data, {
            x: "label",
            y: "value",
            stroke: color,
            strokeWidth: 2,
            points: data.length <= 24,
          }),
        ],
        scales: {
          x: {
            scale: () => scalePoint<string>().padding(0.15),
            axis: {
              line: false,
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
    <ChartContainer className={className}>
      <Chart definition={definition} height={height} ariaLabel={ariaLabel} />
    </ChartContainer>
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
    return defineChart(
      {
        theme: RUNWAY_CHART_THEME,
        margin: { top: 8, right: 10, bottom: 26, left: 36 },
        marks: [
          barY(data, {
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
    <ChartContainer className={className}>
      <Chart definition={definition} height={height} ariaLabel={ariaLabel} />
    </ChartContainer>
  )
}

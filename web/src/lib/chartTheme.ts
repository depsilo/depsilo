/**
 * Recharts takes styling through props rather than classes, so the semantic
 * chart tokens are handed to it here, once. The values are CSS custom
 * properties rather than literals: Recharts writes them straight onto the SVG,
 * so the browser resolves them and the chart follows the active theme.
 *
 * Charts are still part of the design system — a second set of axis greys
 * declared inside one page is drift, not a local decision.
 */

export const CHART_AXIS_TICK = { fill: 'var(--chart-axis-label)', fontSize: 10 }

export const CHART_AXIS = {
  tick: CHART_AXIS_TICK,
  axisLine: false as const,
  tickLine: false as const,
}

export const CHART_LEGEND = {
  color: 'var(--chart-axis-label)',
  fontSize: 11,
  paddingTop: 4,
}

export const CHART_GRID_STROKE = 'var(--chart-grid)'

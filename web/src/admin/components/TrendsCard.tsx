// Tabbed trends chart. One backend request per range carries every
// dimension a tab could render (requests / bandwidth / latency / errors),
// so switching tabs is a pure re-render — no refetch, no jitter.
//
// API increments are aggregated into UTC-aligned display buckets. Bucket timestamps are rendered in the
// browser's timezone via Intl.DateTimeFormat.
import { aggregateTrends } from "@/admin/trendData";
import { ChartNoAxesCombined } from "lucide-react";
import type { ReactNode } from "react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router";
import {
  ComposedChart,
  Bar,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from "recharts";
import type { TooltipContentProps, TooltipValueType } from "recharts";

import ButtonV2 from "@/components/app/button";
import Modal from "@/components/app/modal";
import SectionHeader from "@/components/app/section-header";
import { useMediaQuery } from "@/hooks/useMediaQuery";
import { CHART_AXIS, CHART_GRID_STROKE } from "@/lib/chartTheme";
import { cn, formatBytes } from "@/lib/utils";

export type TrendsRange = "1h" | "24h" | "7d" | "30d";
export type TrendsTab = "requests" | "bandwidth" | "latency" | "errors";

export interface RawTrendPoint {
  bucket: number;
  date: string;
  requests: number;
  hits: number;
  misses: number;
  hit_rate: number;
  bytes_served: number;
  bytes_hit: number;
  bytes_miss: number;
  sum_latency_ms: number;
  avg_latency_ms: number;
  errors: number;
}

const TZ = Intl.DateTimeFormat().resolvedOptions().timeZone;

function fmtAxisTime(
  bucket: number,
  granularity: "minute" | "hour" | "day",
  locale: string,
): string {
  const d = new Date(bucket * 1000);
  if (granularity === "minute") {
    return d.toLocaleTimeString(locale, {
      hour: "2-digit",
      minute: "2-digit",
      timeZone: TZ,
    });
  }
  if (granularity === "hour") {
    // Include short date so the 24h tail across midnight is unambiguous.
    return d.toLocaleString(locale, {
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      timeZone: TZ,
    });
  }
  // day
  return d.toLocaleDateString(locale, {
    month: "2-digit",
    day: "2-digit",
    timeZone: TZ,
  });
}

function fmtTooltipTime(
  bucket: number,
  range: TrendsRange,
  end?: number,
  locale?: string,
): string {
  const d = new Date(bucket * 1000);
  const start = d.toLocaleString(locale, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: range === "1h" ? "2-digit" : undefined,
    timeZoneName: "short",
    timeZone: TZ,
  });
  if (!end) return start;
  const finish = new Date(end * 1000).toLocaleString(locale, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    timeZoneName: "short",
    timeZone: TZ,
  });
  return `${start} – ${finish}`;
}

interface Props {
  raw: RawTrendPoint[];
  range: TrendsRange;
  dataRange: TrendsRange;
  /** Optional dashboard summary rendered alongside the chart controls. */
  summary?: ReactNode;
  isFetching?: boolean;
  isStale?: boolean;
  onRetry?: () => void;
  onRangeChange?: (r: TrendsRange) => void;
}

const RANGES: { value: TrendsRange; key: string }[] = [
  { value: "1h", key: "range1h" },
  { value: "24h", key: "range24h" },
  { value: "7d", key: "range7d" },
  { value: "30d", key: "range30d" },
];

const TABS: { value: TrendsTab; key: string }[] = [
  { value: "requests", key: "trendTabRequests" },
  { value: "bandwidth", key: "trendTabBandwidth" },
  { value: "latency", key: "trendTabLatency" },
  { value: "errors", key: "trendTabErrors" },
];

interface ChartTooltipProps extends TooltipContentProps<
  TooltipValueType,
  string | number
> {
  dataRange: TrendsRange;
}

function ChartTooltip({
  active,
  payload,
  label,
  dataRange,
}: ChartTooltipProps) {
  const { t, i18n } = useTranslation();
  if (!active || !payload?.length) return null;
  const payloadPoint = payload[0]?.payload as
    ReturnType<typeof aggregateTrends>[number] | undefined;
  const formattedLabel =
    typeof label === "number"
      ? fmtTooltipTime(
          payloadPoint?.bucketStart ?? label,
          dataRange,
          payloadPoint?.bucketEnd,
          i18n.language,
        )
      : label;
  return (
    <div className="max-w-[min(32rem,calc(100vw-2rem))] rounded-md border border-border bg-card px-3 py-2 text-sm shadow-sm">
      <p className="mb-1 font-normal text-foreground">{formattedLabel}</p>
      {payloadPoint?.partial && (
        <p className="text-sm text-muted-foreground">
          {t("dashboard.partialBucket")}
        </p>
      )}
      {payloadPoint?.missing && (
        <p className="text-sm text-warning">{t("dashboard.missingBuckets")}</p>
      )}
      {payload.some((entry) => entry.dataKey === "hits") && (
        <p className="tabular-nums">
          {t("dashboard.hitRate")}:{" "}
          {payloadPoint?.hit_rate_pct == null
            ? "—"
            : `${payloadPoint.hit_rate_pct.toFixed(1)}%`}
        </p>
      )}
      {payload.map((entry) => {
        const v = entry.value;
        let display: string;
        if (
          v === null ||
          v === undefined ||
          v === "" ||
          !Number.isFinite(Number(v))
        ) {
          display = `— (${t("dashboard.chartNoDataTooltip")})`;
        } else if (
          entry.dataKey === "hit_rate_pct" ||
          entry.dataKey === "error_rate_pct"
        ) {
          display = `${Number(v).toFixed(1)}%`;
        } else if (
          entry.dataKey === "bytes_hit" ||
          entry.dataKey === "bytes_miss" ||
          entry.dataKey === "bytes_total"
        ) {
          display = formatBytes(Number(v));
        } else if (entry.dataKey === "avg_latency_ms") {
          display = `${Number(v).toLocaleString()} ${t("dashboard.msUnit")}`;
        } else {
          display = Number(v).toLocaleString();
        }
        return (
          <p
            key={String(entry.dataKey)}
            className="tabular-nums"
            style={{ color: entry.color }}
          >
            {entry.name}: {display}
          </p>
        );
      })}
    </div>
  );
}

const axisProps = { ...CHART_AXIS, tick: { ...CHART_AXIS.tick, fontSize: 13 } };

export default function TrendsCard({
  raw,
  range,
  dataRange,
  summary,
  isFetching = false,
  isStale = false,
  onRetry,
  onRangeChange,
}: Props) {
  const { t, i18n } = useTranslation();
  const [search, setSearch] = useSearchParams();
  const tab =
    TABS.find((item) => item.value === search.get("metric"))?.value ??
    "requests";
  const setTab = (value: TrendsTab) =>
    setSearch((previous) => {
      const next = new URLSearchParams(previous);
      next.set("metric", value);
      return next;
    });
  const [chartInfoOpen, setChartInfoOpen] = useState(false);
  const isMobile = useMediaQuery("(max-width: 640px)");
  const granularity =
    dataRange === "1h" || dataRange === "24h" ? "minute" : "day";

  const points = useMemo(
    () => aggregateTrends(raw, dataRange),
    [raw, dataRange],
  );

  const hasMissing = points.some((point) => point.missing);
  const allEmpty =
    !hasMissing &&
    (points.length === 0 ||
      points.every((point) =>
        tab === "requests"
          ? !point.requests
          : tab === "bandwidth"
            ? !point.bytes_total
            : tab === "latency"
              ? point.avg_latency_ms === null
              : !point.errors,
      ));
  const activeTab = TABS.find((item) => item.value === tab);
  const activeRange = RANGES.find((item) => item.value === dataRange);
  const selectedRange = RANGES.find((item) => item.value === range);
  const chartDescription = t("dashboard.trendChartDescription", {
    metric: activeTab ? t(`dashboard.${activeTab.key}`) : "",
    range: activeRange ? t(`dashboard.${activeRange.key}`) : "",
  });

  return (
    <section
      data-dashboard-panel
      aria-busy={isFetching || undefined}
      className="min-w-0 overflow-hidden rounded-lg border border-border bg-card font-sans shadow-[0_2px_10px_rgba(40,74,111,.055)]"
    >
      <div className="px-4 pt-3 [&>header]:flex-wrap [&>header>div]:min-w-0">
        <SectionHeader
          title={t("dashboard.trafficTrend")}
          divider={false}
          hint={undefined}
          action={
            <div className="flex flex-wrap items-center justify-end gap-2">
              {summary && (
                <span className="tabular-nums text-sm text-muted-foreground">
                  {summary}
                </span>
              )}
              <ButtonV2
                type="button"
                variant="ghost"
                size="sm"
                className="size-9 p-0 text-sm text-muted-foreground"
                onClick={() => setChartInfoOpen(true)}
                aria-haspopup="dialog"
                aria-label={t("dashboard.chartWindowInfo")}
              >
                ⓘ
              </ButtonV2>
              {onRangeChange ? (
                <div
                  data-trend-range-control
                  className="grid grid-cols-4 overflow-hidden rounded-sm border-[0.5px] border-border bg-muted sm:flex"
                  role="group"
                  aria-label={t("dashboard.hitMissTrend")}
                >
                  {RANGES.map((r) => {
                    const active = range === r.value;
                    return (
                      <button
                        key={r.value}
                        type="button"
                        onClick={() => onRangeChange(r.value)}
                        aria-pressed={active}
                        className={cn(
                          "min-h-10 min-w-0 cursor-pointer rounded-sm border px-2 text-sm font-medium whitespace-nowrap transition-colors sm:px-2.5",
                          active
                            ? "border-input bg-card text-foreground"
                            : "border-transparent text-muted-foreground hover:text-foreground",
                        )}
                      >
                        {t(`dashboard.${r.key}`)}
                      </button>
                    );
                  })}
                </div>
              ) : (
                <span className="rounded-sm bg-muted px-2.5 py-1.5 text-sm font-medium text-muted-foreground">
                  {selectedRange ? t(`dashboard.${selectedRange.key}`) : null}
                </span>
              )}
            </div>
          }
        />
      </div>

      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 border-t border-border/60 px-4 pb-2 pt-1">
        <div
          className="flex flex-wrap items-center gap-1"
          role="group"
          aria-label={t("dashboard.trendMetricGroup")}
        >
          {TABS.map((tb) => {
            const active = tab === tb.value;
            return (
              <button
                key={tb.value}
                type="button"
                onClick={() => setTab(tb.value)}
                aria-pressed={active}
                className={cn(
                  "min-h-10 min-w-0 cursor-pointer rounded-sm border-b-2 border-transparent bg-transparent px-2 text-sm whitespace-nowrap transition-colors sm:px-2.5",
                  active
                    ? "border-b-primary font-semibold text-foreground"
                    : "font-medium text-muted-foreground hover:text-foreground",
                )}
              >
                {t(`dashboard.${tb.key}`)}
              </button>
            );
          })}
        </div>
        {(tab === "requests" || tab === "bandwidth") && (
          <div
            className="flex flex-wrap items-center gap-4 text-sm text-muted-foreground"
            aria-label={t("dashboard.trendMetricGroup")}
          >
            <span className="inline-flex items-center gap-1.5">
              <span aria-hidden className="dashboard-trend-service size-2 bg-current" />
              {t(
                tab === "requests"
                  ? "dashboard.requestsLabel"
                  : "dashboard.hitResponseBytes",
              )}
            </span>
            {tab === "bandwidth" && (
              <span className="inline-flex items-center gap-1.5">
                <span aria-hidden className="dashboard-trend-miss size-2 bg-current" />
                {t("dashboard.missResponseBytes")}
              </span>
            )}
          </div>
        )}
      </div>
      {range !== dataRange && (
        <p className="px-4 pb-2 text-sm text-warning" role="status">
          {t("dashboard.chartRetainedRange", {
            shown: t(
              `dashboard.${RANGES.find((item) => item.value === dataRange)!.key}`,
            ),
            requested: t(
              `dashboard.${RANGES.find((item) => item.value === range)!.key}`,
            ),
          })}
        </p>
      )}
      {isStale && (
        <div
          className="mx-4 mb-3 flex flex-wrap items-center justify-between gap-2 rounded-sm bg-warning/10 px-3 py-2 text-sm text-warning"
          role="status"
        >
          <span>{t("now.staleData")}</span>
          {onRetry && (
            <ButtonV2
              type="button"
              variant="secondary"
              size="sm"
              onClick={onRetry}
            >
              {t("now.refresh")}
            </ButtonV2>
          )}
        </div>
      )}

      {hasMissing && (
        <p role="status" className="px-4 pb-2 text-sm text-warning">
          {t("dashboard.missingBuckets")}
        </p>
      )}
      {allEmpty ? (
        <div className="flex min-h-[220px] items-center justify-center gap-3 px-4 pb-4 text-left">
          <span
            aria-hidden
            className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-sm bg-muted text-muted-foreground"
          >
            <ChartNoAxesCombined className="icon icon-sm" aria-hidden="true" />
          </span>
          <div className="min-w-0">
            <h3 className="text-sm font-semibold text-foreground">
              {t(
                tab === "errors"
                  ? "dashboard.emptyErrorsTitle"
                  : tab === "latency"
                    ? "dashboard.emptyLatencyTitle"
                    : "dashboard.emptyTrendTitle",
                {
                  range: t(
                    `dashboard.${RANGES.find((item) => item.value === dataRange)!.key}`,
                  ),
                },
              )}
            </h3>
            <p className="mt-1 max-w-[52ch] text-sm leading-[1.5] text-muted-foreground">
              {tab === "requests" || tab === "bandwidth"
                ? t("dashboard.emptyTrendHint")
                : null}
            </p>
          </div>
        </div>
      ) : (
        <div className="px-2 pb-3">
          <ResponsiveContainer width="100%" height={240}>
            <ComposedChart
              data={points}
              margin={{ top: 4, right: 12, bottom: 0, left: 0 }}
              desc={chartDescription}
            >
              <CartesianGrid stroke={CHART_GRID_STROKE} vertical={false} />
              <XAxis
                dataKey="bucket"
                type="number"
                domain={[
                  points[0]?.bucket ?? "dataMin",
                  points[points.length - 1]?.bucketEnd ?? "dataMax",
                ]}
                tickCount={isMobile ? 4 : 6}
                tickFormatter={(value: number) =>
                  fmtAxisTime(value, granularity, i18n.language)
                }
                minTickGap={12}
                {...axisProps}
              />
              <Tooltip
                filterNull={false}
                content={(props) => (
                  <ChartTooltip {...props} dataRange={dataRange} />
                )}
              />
              {tab === "requests" && (
                <>
                  <YAxis
                    yAxisId="count"
                    {...axisProps}
                    allowDecimals={false}
                    tickCount={4}
                    domain={[0, "auto"]}
                    width={40}
                  />
                  <Bar
                    yAxisId="count"
                    dataKey="requests"
                    fill="var(--dashboard-service)"
                    fillOpacity={0.84}
                    name={t("dashboard.requestsLabel")}
                    isAnimationActive={false}
                    maxBarSize={32}
                  />
                </>
              )}

              {tab === "bandwidth" && (
                <>
                  <YAxis
                    yAxisId="bytes"
                    tickFormatter={(v: number) => formatBytes(v)}
                    {...axisProps}
                    width={56}
                  />
                  <Bar
                    yAxisId="bytes"
                    dataKey="bytes_hit"
                    stackId="bytes"
                    fill="var(--dashboard-service)"
                    fillOpacity={0.84}
                    name={t("dashboard.hitResponseBytes")}
                    isAnimationActive={false}
                    maxBarSize={32}
                  />
                  <Bar
                    yAxisId="bytes"
                    dataKey="bytes_miss"
                    stackId="bytes"
                    fill="var(--dashboard-muted)"
                    fillOpacity={0.48}
                    name={t("dashboard.missResponseBytes")}
                    isAnimationActive={false}
                    maxBarSize={32}
                  />
                </>
              )}

              {tab === "latency" && (
                <>
                  <YAxis
                    yAxisId="ms"
                    {...axisProps}
                    width={48}
                    tickFormatter={(v: number) => `${v}ms`}
                  />
                  <Line
                    yAxisId="ms"
                    type="linear"
                    dataKey="avg_latency_ms"
                    stroke="var(--dashboard-service)"
                    strokeWidth={1.8}
                    dot={false}
                    name={t("dashboard.avgLatency")}
                    isAnimationActive={false}
                  />
                </>
              )}

              {tab === "errors" && (
                <>
                  <YAxis
                    yAxisId="count"
                    {...axisProps}
                    allowDecimals={false}
                    tickCount={4}
                    domain={[0, "auto"]}
                    width={40}
                  />
                  <Bar
                    yAxisId="count"
                    dataKey="errors"
                    fill="var(--destructive)"
                    fillOpacity={0.78}
                    name={t("dashboard.trendTabErrors")}
                    isAnimationActive={false}
                    maxBarSize={32}
                  />
                </>
              )}
            </ComposedChart>
          </ResponsiveContainer>
        </div>
      )}
      <Modal
        open={chartInfoOpen}
        onClose={() => setChartInfoOpen(false)}
        title={t("dashboard.trafficTrend")}
        width={520}
      >
        <p className="text-sm leading-5 text-muted-foreground">
          {t("dashboard.chartWindowInfo")}
        </p>
        <p className="text-sm leading-5 text-muted-foreground">
          {t("dashboard.trendAggregation")}
        </p>
      </Modal>
    </section>
  );
}

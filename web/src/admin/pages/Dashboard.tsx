import { useEffect, useState } from "react";
import { useSearchParams } from "react-router";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Activity, CheckCircle2, Clock3, Server } from "lucide-react";
import AdminPage from "@/admin/components/AdminPage";
import DashboardHeader from "@/admin/components/DashboardHeader";
import DashboardAttention from "@/admin/components/DashboardAttention";
import TrendsCard, {
  type RawTrendPoint,
  type TrendsRange,
} from "@/admin/components/TrendsCard";
import {
  CacheBenefits,
  RecentRequests,
  RequestDetailsDialog,
  RuntimeResources,
  TrafficOverview,
  UpstreamHealthSummary,
} from "@/admin/components/DashboardPanels";
import type {
  AccessLog,
  BandwidthReportResponse,
  NowResponse,
  PolicyStatus,
} from "@/lib/adminApi.types";
import { adminApi, statsApi } from "@/lib/api";
import { getApiError } from "@/lib/apiError";
import { dashboardStatus } from "@/admin/dashboardStatus";
import { cn } from "@/lib/utils";
import QueryErrorState from "@/components/app/error-state";
import TrafficAnalysis from "@/admin/components/TrafficAnalysis";
import styles from "./Dashboard.module.css";

const RANGES: TrendsRange[] = ["1h", "24h", "7d", "30d"];
const RANGE_LABELS: Record<TrendsRange, string> = {
  "1h": "1h",
  "24h": "24h",
  "7d": "7d",
  "30d": "30d",
};

function formatLastActivity(
  seconds: number,
  t: (key: string, options?: Record<string, unknown>) => string,
) {
  if (seconds < 60) return t("now.justNow");
  if (seconds < 3600)
    return t("now.minutesAgo", { count: Math.floor(seconds / 60) });
  if (seconds < 86400)
    return t("now.hoursAgo", { count: Math.floor(seconds / 3600) });
  return t("now.daysAgo", { count: Math.floor(seconds / 86400) });
}

function formatUptime(
  seconds: number | undefined,
  t: (key: string, options?: Record<string, unknown>) => string,
) {
  if (seconds == null) return "—";
  if (seconds < 60) return t("dashboard.uptimeUnderMinute");
  if (seconds < 3600)
    return t("dashboard.uptimeMinutes", { count: Math.floor(seconds / 60) });
  if (seconds < 86400)
    return t("dashboard.uptimeHours", { count: Math.floor(seconds / 3600) });
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor(seconds / 3600) % 24;
  return hours
    ? `${t("dashboard.uptimeDays", { count: days })} ${t("dashboard.uptimeHours", { count: hours })}`
    : t("dashboard.uptimeDays", { count: days });
}

export default function DashboardV2() {
  const { t, i18n } = useTranslation();
  const [search, setSearch] = useSearchParams();
  const rawRange = search.get("range") ?? search.get("trendRange");
  const range: TrendsRange = RANGES.includes(rawRange as TrendsRange)
    ? (rawRange as TrendsRange)
    : "30d";
  const reportSupported = range === "7d" || range === "30d";
  const [selectedLog, setSelectedLog] = useState<AccessLog | null>(null);
  const [statusDetailsOpen, setStatusDetailsOpen] = useState(false);
  const [loadedTrendRange, setLoadedTrendRange] = useState<TrendsRange>(range);
  const [retainedTrendPoints, setRetainedTrendPoints] = useState<
    RawTrendPoint[] | undefined
  >();
  const dashboardQuery = useQuery({
    queryKey: ["admin", "dashboard"],
    queryFn: ({ signal }) => adminApi.getDashboard({ signal }),
    refetchInterval: 30_000,
    retry: false,
  });
  const nowQuery = useQuery<NowResponse>({
    queryKey: ["admin", "now"],
    queryFn: ({ signal }) =>
      statsApi.getNow({ signal }).then((response) => response.data),
    refetchInterval: 5_000,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: "always",
    staleTime: 4_000,
    retry: false,
  });
  const policyQuery = useQuery<PolicyStatus>({
    queryKey: ["admin", "policy", "status"],
    queryFn: ({ signal }) =>
      adminApi.getPolicyStatus({ signal }).then((response) => response.data),
    refetchInterval: 30_000,
    retry: false,
  });
  const trendsQuery = useQuery({
    queryKey: ["admin", "dashboard", "trends", range],
    queryFn: ({ signal }) => adminApi.getDashboardTrends(range, { signal }),
    placeholderData: keepPreviousData,
    refetchInterval: range === "1h" ? 5_000 : 30_000,
    refetchOnWindowFocus: "always",
    retry: false,
  });
  const reportQuery = useQuery<BandwidthReportResponse>({
    queryKey: ["admin", "dashboard", "period", range],
    queryFn: ({ signal }) =>
      adminApi
        .getBandwidthReport({ range }, { signal })
        .then((response) => response.data),
    placeholderData: keepPreviousData,
    refetchInterval: 60_000,
    enabled: reportSupported,
    retry: false,
  });
  const requestsQuery = useQuery({
    queryKey: ["admin", "dashboard", "requests"],
    queryFn: ({ signal }) =>
      adminApi
        .listLogs({ page: 1, page_size: 5 }, { signal })
        .then((response) => response.data),
    refetchInterval: 30_000,
    retry: false,
  });

  useEffect(() => {
    if (
      trendsQuery.data &&
      !trendsQuery.isPlaceholderData &&
      !trendsQuery.isRefetchError &&
      !trendsQuery.isFetching
    ) {
      // Query state is the external source of truth for the retained range.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setLoadedTrendRange(range);
      setRetainedTrendPoints(
        (trendsQuery.data.data.points ?? []) as RawTrendPoint[],
      );
    }
  }, [
    range,
    trendsQuery.data,
    trendsQuery.isFetching,
    trendsQuery.isPlaceholderData,
    trendsQuery.isRefetchError,
  ]);

  const dashboard = dashboardQuery.data?.data;
  const now = nowQuery.data;
  const {
    upstreamHealth,
    attention: upstreamsNeedingAttention,
    policyState,
    overall,
    issueCategories,
  } = dashboardStatus(dashboard, policyQuery.data, {
    snapshotError: dashboardQuery.isError,
    policyError: policyQuery.isError,
    proxyResponding: Boolean(now && !nowQuery.isError),
  });
  const points = (trendsQuery.data?.data.points ??
    retainedTrendPoints ??
    []) as RawTrendPoint[];
  const period =
    trendsQuery.isError ||
    loadedTrendRange !== range ||
    trendsQuery.isPlaceholderData
      ? undefined
      : points.reduce(
          (total, point) => ({
            requests: total.requests + point.requests,
            hits: total.hits + point.hits,
            bytes: total.bytes + point.bytes_served,
          }),
          { requests: 0, hits: 0, bytes: 0 },
        );
  const report =
    reportSupported &&
    !reportQuery.isPlaceholderData &&
    !reportQuery.isRefetchError
      ? reportQuery.data
      : undefined;
  const trendBusy =
    trendsQuery.isFetching ||
    (!trendsQuery.isError &&
      !trendsQuery.isRefetchError &&
      loadedTrendRange !== range);
  const updatedAt = dashboardQuery.dataUpdatedAt || nowQuery.dataUpdatedAt;
  const updatedLabel = updatedAt
    ? new Date(updatedAt).toLocaleTimeString(i18n.language, {
        hour: "2-digit",
        minute: "2-digit",
      })
    : "—";
  const refresh = () => {
    void Promise.all([
      dashboardQuery.refetch(),
      nowQuery.refetch(),
      policyQuery.refetch(),
      ...(reportSupported ? [reportQuery.refetch()] : []),
      requestsQuery.refetch(),
    ]);
  };
  const changeRange = (next: TrendsRange) => {
    setSearch(() => {
      // Read the latest URL so a metric-tab update from TrendsCard cannot be
      // lost when the header period control is clicked immediately after it.
      const params = new URLSearchParams(window.location.search);
      params.set("range", next);
      params.delete("trendRange");
      return params;
    });
  };

  return (
    <AdminPage title={false}>
      <div
        data-dashboard-overview
        className={cn(
          styles.overview,
          "mx-auto w-full max-w-[1600px] space-y-3",
        )}
      >
        <DashboardHeader
          upstreamHealth={upstreamHealth}
          policyState={policyState}
          proxyResponding={Boolean(now && !nowQuery.isError)}
          snapshotUpdatedAt={dashboardQuery.dataUpdatedAt}
          policyUpdatedAt={policyQuery.dataUpdatedAt}
          proxyUpdatedAt={nowQuery.dataUpdatedAt}
          snapshotError={dashboardQuery.isError}
          policyError={policyQuery.isError}
          proxyError={nowQuery.isError}
          uptimeSeconds={now?.uptime_seconds}
          refreshing={
            dashboardQuery.isFetching ||
            nowQuery.isFetching ||
            policyQuery.isFetching
          }
          onRefresh={refresh}
          updatedAt={updatedLabel}
          statusDetailsOpen={statusDetailsOpen}
          onStatusDetailsChange={setStatusDetailsOpen}
          periodControl={
            <div
              className="inline-flex rounded-md border border-border bg-card p-1"
              role="group"
              aria-label={t("dashboard.period")}
            >
              {RANGES.map((item) => (
                <button
                  key={item}
                  type="button"
                  className={cn(
                    "min-h-9 rounded px-3 text-sm",
                    range === item
                      ? "bg-accent font-semibold text-foreground"
                      : "text-muted-foreground",
                  )}
                  aria-pressed={range === item}
                  onClick={() => changeRange(item)}
                >
                  {RANGE_LABELS[item]}
                </button>
              ))}
            </div>
          }
        />
        <div data-query-key="dashboard-snapshot">
          <section
            data-query-key="now"
            data-dashboard-panel
            data-dashboard-status-strip
            className="rounded-lg border border-border bg-card p-4"
          >
          <div className="grid gap-5 sm:grid-cols-2 xl:grid-cols-4">
            <button
              type="button"
              className="group flex min-w-0 items-center gap-3 py-1 text-left focus-visible:outline-2 focus-visible:outline-ring focus-visible:outline-offset-2"
              aria-haspopup="dialog"
              aria-expanded={statusDetailsOpen}
              onClick={() => setStatusDetailsOpen(true)}
            >
              <span
                data-dashboard-status-icon
                className={cn(
                  overall === "healthy"
                    ? "dashboard-status-ok"
                    : overall === "attention"
                      ? "dashboard-status-attention"
                      : "dashboard-status-unknown",
                )}
                aria-hidden="true"
              >
                {overall === "healthy" ? (
                  <CheckCircle2 className="size-4" />
                ) : (
                  <Server className="size-4" />
                )}
              </span>
              <div className="min-w-0 break-words">
                <p className="text-sm text-muted-foreground">
                  {t("dashboard.serviceStatus")}
                </p>
                <p
                  className={cn(
                    "mt-1 text-lg font-semibold",
                    overall === "healthy"
                      ? "text-success"
                      : overall === "attention"
                        ? "text-warning"
                        : "text-muted-foreground",
                  )}
                >
                  {t(
                    overall === "healthy"
                      ? "dashboard.overallHealthy"
                      : overall === "attention"
                        ? "dashboard.overallAttention"
                        : "dashboard.overallUnknown",
                  )}
                </p>
                <div className="mt-2">
                  <UpstreamHealthSummary
                    upstreams={dashboard?.upstreams ?? []}
                    known={Boolean(dashboard)}
                  />
                </div>
              </div>
            </button>
            <div>
              <span data-dashboard-status-icon aria-hidden="true">
                <Activity className="size-4" />
              </span>
              <div className="min-w-0 break-words">
                <p className="text-sm text-muted-foreground">
                  {t("dashboard.currentActivity")}
                </p>
                <p className="mt-1 text-lg font-semibold">
                  {now?.rate.state === "ready" ||
                  (now?.rate.state == null && now?.rate.has_data)
                    ? t("dashboard.serving")
                    : now?.rate.state === "sampling"
                      ? t("dashboard.resourceSampling")
                      : nowQuery.isError
                      ? "—"
                      : t("dashboard.idle")}
                </p>
                {nowQuery.isError && (
                  <p role="status" className="text-sm text-warning">
                    {nowQuery.data
                      ? t("dashboard.liveStale")
                      : t("dashboard.liveUnavailable")}
                  </p>
                )}
              </div>
            </div>
            <div>
              <span data-dashboard-status-icon aria-hidden="true">
                <Clock3 className="size-4" />
              </span>
              <div className="min-w-0 break-words">
                <p className="text-sm text-muted-foreground">
                  {t("now.lastActivity")}
                </p>
                <p className="mt-1 text-lg font-semibold">
                  {now?.last_activity?.package_name || "—"}
                </p>
                <p className="text-sm text-muted-foreground">
                  {now?.last_activity
                    ? now.last_activity.seconds_ago < 60
                      ? t("now.justNow")
                      : formatLastActivity(now.last_activity.seconds_ago, t)
                    : "—"}
                </p>
              </div>
            </div>
            <div>
              <span data-dashboard-status-icon aria-hidden="true">
                <Server className="size-4" />
              </span>
              <div className="min-w-0 break-words">
                <p className="text-sm text-muted-foreground">
                  {t("dashboard.uptime")}
                </p>
                <p className="mt-1 text-lg font-semibold tabular-nums">
                  {formatUptime(now?.uptime_seconds, t)}
                </p>
              </div>
            </div>
          </div>
          </section>
        </div>
        <div className={styles.metricPanels} data-dashboard-metric-panels>
          <RuntimeResources dashboard={dashboard} now={now} />
          <TrafficOverview now={now} period={period} />
        </div>
        <div className="grid gap-5 xl:grid-cols-[minmax(0,2fr)_minmax(280px,1fr)]">
          <CacheBenefits report={report} period={period} />
          <DashboardAttention
            issues={issueCategories}
            isPending={dashboardQuery.isPending}
            isFetching={dashboardQuery.isFetching}
            initialErrorMessage={
              dashboardQuery.isError
                ? getApiError(dashboardQuery.error).message
                : undefined
            }
            isStale={Boolean(
              dashboardQuery.data && dashboardQuery.isRefetchError,
            )}
            upstreams={upstreamsNeedingAttention}
            policyState={policyQuery.isPending ? undefined : policyState}
            onRetryPolicy={() => {
              void policyQuery.refetch();
            }}
            policyRefreshing={policyQuery.isFetching}
            cacheUsagePercent={dashboard?.cache_usage_percent}
            onRetry={() => {
              void dashboardQuery.refetch();
            }}
          />
        </div>
        <div className="grid min-w-0 gap-5 2xl:grid-cols-[minmax(0,7fr)_minmax(0,5fr)]">
          <section
            data-query-key="dashboard-trends"
            aria-busy={trendBusy || undefined}
            className="min-w-0"
          >
            {trendsQuery.isError &&
            !trendsQuery.data &&
            retainedTrendPoints === undefined ? (
              <QueryErrorState
                message={t("dashboard.trendsUnavailable")}
                onRetry={() => {
                  void trendsQuery.refetch();
                }}
              />
            ) : (
              <TrendsCard
                raw={points}
                range={range}
                dataRange={loadedTrendRange}
                summary={
                  period && (
                    <span className="tabular-nums">
                      {period.requests.toLocaleString()}{" "}
                      {t("dashboard.requestsLabel")}
                    </span>
                  )
                }
                isFetching={trendBusy}
                isStale={trendsQuery.isRefetchError}
                onRetry={() => {
                  void trendsQuery.refetch();
                }}
              />
            )}
          </section>
          <RecentRequests
            items={requestsQuery.data?.items ?? []}
            onSelect={setSelectedLog}
          />
        </div>
        {search.has("analysisRange") && <TrafficAnalysis />}
        <RequestDetailsDialog
          item={selectedLog}
          onClose={() => setSelectedLog(null)}
        />
      </div>
    </AdminPage>
  );
}

import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import {
  ArrowDownToLine,
  ArrowLeftRight,
  Cpu,
  Database,
  HardDrive,
  Network,
  PieChart,
  Server,
  Timer,
  Zap,
} from "lucide-react";
import { Link } from "react-router";
import Modal from "@/components/app/modal";
import Badge from "@/components/app/badge";
import EcosystemIcon from "@/components/app/ecosystem-icon";
import type {
  AccessLog,
  AccessLogDetail,
  DashboardResponse,
  DashboardUpstream,
  NowResponse,
  BandwidthReportResponse,
} from "@/lib/adminApi.types";
import { formatBytes } from "@/lib/utils";
import { getAdminRouteHref } from "@/admin/routes";
import { adminApi } from "@/lib/api";
import { useQuery } from "@tanstack/react-query";

function valueOrDash(
  value: number | null | undefined,
  format: (v: number) => string,
) {
  return typeof value === "number" && Number.isFinite(value)
    ? format(value)
    : "—";
}

function displayMetric(value: string) {
  const match = value.match(/^(.*?)(\s+)(\S+)$/);
  if (!match || value === "—") return value;
  return (
    <>
      <span data-dashboard-value-number>{match[1]}</span>
      <span data-dashboard-value-unit>{match[3]}</span>
    </>
  );
}

function rateStatus(
  rate: NowResponse["rate"] | undefined,
  upstream = false,
) {
  if (!rate) return "unavailable";
  return upstream ? (rate.upstream_state ?? "unavailable") : (rate.state ?? (rate.has_data ? "ready" : "sampling"));
}

function rateText(
  rate: NowResponse["rate"] | undefined,
  t: (key: string, options?: Record<string, unknown>) => string,
  upstream = false,
) {
  const state = rateStatus(rate, upstream);
  if (state === "sampling") return t("dashboard.resourceSampling");
  if (rate?.error || state === "error") return t("dashboard.resourceError");
  if (state === "unavailable") return t("dashboard.resourceUnavailable");
  if (upstream && rate?.upstream_bps == null) {
    return t("dashboard.upstreamTrafficUnavailable");
  }
  return t("dashboard.sampledWindow", {
    seconds: upstream ? rate?.upstream_coverage_seconds ?? 60 : rate?.coverage_seconds ?? 60,
  });
}

function requestsPerSecond(
  rate: NowResponse["rate"] | undefined,
  upstream = false,
) {
  if (!rate || rateStatus(rate, upstream) !== "ready") return null;
  const value = upstream
    ? rate.upstream_requests_per_second ??
      (rate.upstream_requests_per_min == null ? null : rate.upstream_requests_per_min / 60)
    : rate.requests_per_second ?? rate.requests_per_min / 60;
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function formatRequestsPerSecondValue(value: number | null) {
  if (value == null) return "—";
  return value > 0 && value < 0.1 ? "<0.1" : value.toFixed(1);
}

function RequestSparkline({ points }: { points: Array<{ requests: number }> }) {
  if (points.length < 2) return null;
  const values = points.map((point) => point.requests);
  if (values.every((value) => value <= 0)) return null;
  const max = Math.max(...values, 1);
  const path = values
    .map((value, index) => {
      const x = (index / (values.length - 1)) * 100;
      const y = 18 - (value / max) * 15;
      return `${index === 0 ? "M" : "L"}${x.toFixed(2)} ${y.toFixed(2)}`;
    })
    .join(" ");
  return (
    <svg
      aria-hidden="true"
      className="dashboard-resource-sparkline"
      viewBox="0 0 100 20"
      preserveAspectRatio="none"
    >
      <path d={path} fill="none" vectorEffect="non-scaling-stroke" />
    </svg>
  );
}

function Panel({
  title,
  children,
  className = "",
}: {
  title: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section
      data-dashboard-panel
      data-dashboard-section
      className={`rounded-lg border border-border bg-card p-4 shadow-[0_1px_2px_rgba(20,40,80,.04)] lg:p-5 ${className}`}
    >
      <h2
        aria-label={title === "Traffic overview" ? "Traffic" : undefined}
        className="mb-4 text-lg font-semibold"
      >
        {title}
      </h2>
      {children}
    </section>
  );
}

export function RuntimeResources({
  dashboard,
  now,
}: {
  dashboard?: DashboardResponse;
  now?: NowResponse;
}) {
  const { t } = useTranslation();
  const runtime = dashboard?.runtime;
  const cpu = runtime?.cpu;
  const memory = runtime?.memory;
  const cacheUsage = dashboard?.cache_usage;
  const cpuValue = cpu?.state === "ready" && cpu.percent != null ? `${cpu.percent.toFixed(1)}%` : "—";
  const cpuDetail =
    cpu?.state === "sampling"
      ? t("dashboard.resourceSampling")
      : cpu?.state === "unsupported"
        ? t("dashboard.resourceUnsupported")
        : cpu?.state === "error"
          ? t("dashboard.resourceError")
          : cpu?.state === "ready"
            ? t("dashboard.cpuBasis")
            : t("dashboard.resourceUnavailable");
  const memoryBytes =
    memory?.rss_bytes != null
      ? memory.rss_bytes
      : runtime?.heap_alloc_bytes;
  const memoryDetail =
    memory?.state === "ready" && memory.rss_bytes != null
      ? t("dashboard.processRss")
      : memory?.state === "sampling"
        ? t("dashboard.resourceSampling")
        : memory?.state === "unsupported"
        ? t("dashboard.resourceUnsupported")
        : memory?.state === "error"
          ? t("dashboard.resourceError")
          : runtime
            ? t("dashboard.goHeap")
            : t("dashboard.resourceUnavailable");
  const cachePercent =
    cacheUsage?.state === "ready" && cacheUsage.used_bytes != null && cacheUsage.quota_bytes
      ? (cacheUsage.used_bytes / cacheUsage.quota_bytes) * 100
      : dashboard?.cache_usage_percent;
  const cacheDetail =
    cacheUsage?.state === "ready"
      ? cacheUsage.used_bytes != null && cacheUsage.quota_bytes != null
        ? `${formatBytes(cacheUsage.used_bytes)} / ${formatBytes(cacheUsage.quota_bytes)}`
        : t("dashboard.cacheQuota")
      : cacheUsage?.state === "unsupported"
        ? t("dashboard.resourceUnsupported")
        : cacheUsage?.state === "error"
          ? t("dashboard.resourceError")
          : t("dashboard.resourceUnavailable");
  const serviceRate = requestsPerSecond(now?.rate);
  const upstreamRate = requestsPerSecond(now?.rate, true);
  const cards = [
    {
      label: t("dashboard.cpu"),
      icon: Cpu,
      tone: "cpu",
      value: cpuValue,
      detail: cpuDetail,
    },
    {
      label: t("dashboard.memory"),
      icon: Server,
      tone: "memory",
      value: valueOrDash(memoryBytes, formatBytes),
      detail: memoryDetail,
    },
    {
      label: t("dashboard.cacheSpace"),
      icon: HardDrive,
      tone: "cache",
      value:
        cachePercent == null
          ? "—"
          : cachePercent > 0 && cachePercent < 0.1
            ? "<0.1%"
            : `${cachePercent.toFixed(1)}%`,
      detail: cacheDetail,
    },
    {
      label: t("dashboard.networkRequests"),
      icon: Network,
      tone: "service",
      value: "—",
      detail: rateText(now?.rate, t),
    },
  ];
  return (
    <Panel
      title={t("dashboard.resourceUsage")}
      className="dashboard-resource-panel"
    >
      <div
        data-dashboard-resource-grid
        className="grid gap-4"
      >
        {cards.map(({ label, icon: Icon, tone, value, detail }) => (
          <div
            key={label}
            data-dashboard-card
            data-dashboard-resource-card
            className={`dashboard-metric-card dashboard-resource-card--${tone} min-w-0 rounded-md p-3.5`}
          >
            <div
              data-dashboard-label
              className="dashboard-metric-heading flex items-center gap-2 text-sm text-muted-foreground"
            >
              <span className={`dashboard-resource-icon dashboard-tone-${tone}`}>
                <Icon className="size-4" aria-hidden />
              </span>
              <span>{label}</span>
            </div>
            <div className="dashboard-metric-content">
            {label === t("dashboard.networkRequests") && (
              <RequestSparkline
                points={rateStatus(now?.rate) === "ready" ? (now?.sparkline ?? []) : []}
              />
            )}
            {label === t("dashboard.networkRequests") ? (
              <div className="dashboard-network-rates">
                <div className="dashboard-network-rate dashboard-network-rate--service">
                  <span>{t("dashboard.serviceRequests")}</span>
                  <strong className="tabular-nums">
                    {displayMetric(
                      serviceRate == null
                        ? "—"
                        : `${formatRequestsPerSecondValue(serviceRate)} ${t("dashboard.requestsPerSecond")}`,
                    )}
                  </strong>
                </div>
                <div className="dashboard-network-rate dashboard-network-rate--upstream">
                  <span>{t("dashboard.upstreamRequests")}</span>
                  <strong className="tabular-nums">
                    {displayMetric(
                      upstreamRate == null
                        ? "—"
                        : `${formatRequestsPerSecondValue(upstreamRate)} ${t("dashboard.requestsPerSecond")}`,
                    )}
                  </strong>
                </div>
              </div>
            ) : (
              <p data-dashboard-value className="font-semibold tabular-nums">
                {displayMetric(value)}
              </p>
            )}
            <p data-dashboard-meta className="mt-1 text-sm text-muted-foreground">
              {detail}
            </p>
            {label === t("dashboard.cacheSpace") && cachePercent != null && (
                <div data-dashboard-progress aria-hidden="true">
                  <span
                    style={{
                      width: `${Math.max(0, Math.min(100, cachePercent ?? 0))}%`,
                    }}
                  />
                </div>
              )}
            </div>
          </div>
        ))}
      </div>
    </Panel>
  );
}

export function TrafficOverview({
  now,
  period,
}: {
  now?: NowResponse;
  period?: { bytes: number };
}) {
  const { t } = useTranslation();
  const cards = [
    {
      label: t("dashboard.serviceTraffic"),
      icon: Network,
      tone: "service",
      value:
        rateStatus(now?.rate) === "ready" && now?.rate.egress_bps != null
          ? formatBytes(now.rate.egress_bps) + "/s"
          : "—",
      detail: rateText(now?.rate, t),
    },
    {
      label: t("dashboard.upstreamTraffic"),
      icon: ArrowLeftRight,
      tone: "upstream",
      value:
        rateStatus(now?.rate, true) === "ready" && now?.rate.upstream_bps != null
          ? formatBytes(now.rate.upstream_bps ?? 0) + "/s"
          : "—",
      detail:
        rateText(now?.rate, t, true),
    },
    {
      label: t("dashboard.downloadTotal"),
      icon: ArrowDownToLine,
      tone: "download",
      value: period ? formatBytes(period.bytes) : "—",
      detail: t("dashboard.selectedPeriod"),
    },
    {
      label: t("dashboard.upstreamTotal"),
      icon: Database,
      tone: "upstream",
      value: "—",
      detail: t("dashboard.upstreamTrafficUnavailable"),
    },
  ];
  return (
    <Panel title={t("dashboard.trafficOverview")}>
      <div
        data-dashboard-traffic-grid
        className="grid gap-4"
      >
        {cards.map((card) => (
          <div
            key={card.label}
            data-dashboard-card
            data-dashboard-traffic-card
            className="dashboard-metric-card min-w-0 rounded-md p-3.5"
          >
            <div data-dashboard-label className="dashboard-metric-heading flex items-center gap-2.5 text-sm text-muted-foreground">
              <span className={`dashboard-traffic-icon dashboard-tone-${card.tone}`}>
                <card.icon className="size-4" aria-hidden />
              </span>
              <span>{card.label}</span>
            </div>
            <div className="dashboard-metric-content">
            <p data-dashboard-value className="font-semibold tabular-nums">
              {displayMetric(card.value)}
            </p>
            <p
              data-dashboard-meta
              className="mt-1 text-sm text-muted-foreground"
            >
              {card.detail}
            </p>
            </div>
          </div>
        ))}
      </div>
    </Panel>
  );
}

export function CacheBenefits({
  report,
  period,
}: {
  report?: BandwidthReportResponse;
  period?: { requests: number; hits: number };
}) {
  const { t } = useTranslation();
  const summary = report?.summary;
  const hitRate =
    period && period.requests > 0 ? period.hits / period.requests : null;
  const hitLatency =
    summary && summary.hit_requests > 0 ? summary.avg_hit_latency : null;
  const missLatency =
    summary && summary.miss_requests > 0 ? summary.avg_miss_latency : null;
  const reduction =
    hitLatency != null &&
    missLatency != null &&
    hitLatency > 0 &&
    missLatency > 0
      ? (1 - hitLatency / missLatency) * 100
      : null;
  const formatLatency = (value: number | null, samples: number) =>
    value == null || samples <= 0
      ? "—"
      : value === 0
        ? "—"
        : value < 1
          ? "<1 ms"
        : `${Math.round(value)} ms`;
  return (
    <Panel title={t("dashboard.cacheBenefits")} className="dashboard-benefits-panel">
      <div className="dashboard-benefits-grid grid gap-5 md:grid-cols-3">
        <div className="dashboard-benefit-card">
          <div className="dashboard-benefit-heading">
            <span className="dashboard-benefit-icon dashboard-tone-cpu" aria-hidden="true">
              <PieChart className="size-5" />
            </span>
            <p className="text-sm text-muted-foreground">
              {t("dashboard.hitRate")}
            </p>
          </div>
          <div className="dashboard-benefit-content">
            <p className="dashboard-benefit-value font-semibold tabular-nums">
              {displayMetric(
                hitRate == null ? "—" : `${(hitRate * 100).toFixed(1)}%`,
              )}
            </p>
            <p className="mt-1 text-sm text-muted-foreground">
              {t("dashboard.hitRateBasis")}
            </p>
          </div>
        </div>
        <div className="dashboard-benefit-card">
          <div className="dashboard-benefit-heading">
            <span className="dashboard-benefit-icon dashboard-tone-upstream" aria-hidden="true">
              <Zap className="size-5" />
            </span>
            <p className="text-sm text-muted-foreground">
              {t("dashboard.estimatedSaved")}
            </p>
          </div>
          <div className="dashboard-benefit-content">
            <p className="dashboard-benefit-value font-semibold tabular-nums">
              {displayMetric(summary ? formatBytes(summary.hit_bytes) : "—")}
            </p>
            <p className="mt-1 text-sm text-muted-foreground">
              {t(
                summary
                  ? "dashboard.estimatedSavedHint"
                  : "dashboard.reportRangeUnavailable",
              )}
            </p>
          </div>
        </div>
        <div className="dashboard-benefit-card">
          <div className="dashboard-benefit-heading">
            <span className="dashboard-benefit-icon dashboard-tone-service" aria-hidden="true">
              <Timer className="size-5" />
            </span>
            <p className="text-sm text-muted-foreground">
              {t("dashboard.responsePerformance")}
            </p>
          </div>
          <div className="dashboard-benefit-content">
            <p className="dashboard-benefit-value font-semibold tabular-nums">
              {displayMetric(
                reduction == null ? "—" : `~${reduction.toFixed(0)}%`,
              )}
            </p>
            <p className="mt-1 text-sm text-muted-foreground">
              <span className="block">
                {t("dashboard.cachedResponseLatency", {
                  value: formatLatency(hitLatency, summary?.hit_requests ?? 0),
                })}
              </span>
              <span className="block">
                {t("dashboard.upstreamResponseLatency", {
                  value: formatLatency(missLatency, summary?.miss_requests ?? 0),
                })}
              </span>
              {reduction == null && (
                <span className="mt-1 block">
                  {hitLatency == null || missLatency == null
                    ? t("dashboard.insufficientSamples")
                    : t("dashboard.latencyPrecisionHint")}
                </span>
              )}
            </p>
          </div>
        </div>
      </div>
    </Panel>
  );
}

export function UpstreamHealthSummary({
  upstreams,
  known = true,
}: {
  upstreams: DashboardUpstream[];
  known?: boolean;
}) {
  const { t } = useTranslation();
  if (!known || upstreams.length === 0) {
    return (
      <span className="inline-flex items-center gap-1.5 text-sm text-muted-foreground">
        <span className="size-2 rounded-full bg-muted-foreground" />
        {t(known ? "dashboard.noUpstreams" : "dashboard.upstreamStatusUnknown")}
      </span>
    );
  }
  const failed = upstreams.filter((item) => !item.healthy).length;
  const reachable = upstreams.length - failed;
  const slow = upstreams.filter(
    (item) => item.healthy && item.avg_latency_ms >= 150,
  ).length;
  return (
    <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
      <Link
        className="inline-flex items-center gap-1.5 text-foreground no-underline"
        to={getAdminRouteHref("upstreams")}
      >
        <span
          className={`size-2 rounded-full ${failed ? "bg-destructive" : slow ? "bg-warning" : "bg-success"}`}
        />
        {t("dashboard.upstreamReachableCount", {
          count: reachable,
        })}
      </Link>
      {(failed || slow) > 0 && (
        <span>
          · {t("dashboard.slowCount", { count: slow })}
          {failed
            ? ` · ${t("dashboard.unavailableCount", { count: failed })}`
            : ""}
        </span>
      )}
    </div>
  );
}

export function RecentRequests({
  items,
  onSelect,
}: {
  items: AccessLog[];
  onSelect: (item: AccessLog) => void;
}) {
  const { t, i18n } = useTranslation();
  return (
    <Panel title={t("dashboard.recentRequests")}>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[560px] text-left text-sm">
          <thead className="text-muted-foreground">
            <tr>
              <th className="pb-3 font-medium">{t("dashboard.package")}</th>
              <th className="pb-3 font-medium">{t("dashboard.ecosystem")}</th>
              <th className="pb-3 font-medium">{t("dashboard.result")}</th>
              <th className="pb-3 font-medium">{t("dashboard.size")}</th>
              <th className="pb-3 font-medium">{t("dashboard.latency")}</th>
              <th className="pb-3 font-medium">{t("dashboard.time")}</th>
            </tr>
          </thead>
          <tbody>
            {items.map((item) => (
              <tr
                key={item.id}
                tabIndex={0}
                onClick={() => onSelect(item)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ")
                    onSelect(item);
                }}
                className="cursor-pointer border-t border-border/70 hover:bg-accent/40 focus-visible:outline-2 focus-visible:outline-ring"
              >
                <td className="py-3 font-medium">{item.package_name || "—"}</td>
                <td className="py-3">
                  <span className="inline-flex items-center gap-1">
                    <EcosystemIcon
                      type={item.adapter_type as never}
                      size={16}
                    />
                    {item.adapter_type || "—"}
                  </span>
                </td>
                <td className="py-3">
                  <Badge
                    variant={
                      item.status_code >= 400
                        ? "destructive"
                        : item.cache_result === "hit"
                          ? "success"
                          : "neutral"
                    }
                  >
                    {item.status_code >= 400
                      ? t("dashboard.failed")
                      : item.cache_result === "hit"
                        ? t("dashboard.cacheHit")
                        : item.cache_result === "miss"
                          ? t("dashboard.upstreamFetch")
                          : t("dashboard.unknown")}
                  </Badge>
                </td>
                <td className="py-3 font-mono tabular-nums">
                  {formatBytes(item.bytes_sent)}
                </td>
                <td className="py-3 font-mono tabular-nums">
                  {item.latency_ms} ms
                </td>
                <td className="py-3 text-muted-foreground">
                  {new Date(item.created_at).toLocaleString(i18n.language, {
                    dateStyle: "short",
                    timeStyle: "short",
                  })}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {items.length === 0 && (
          <p className="py-8 text-center text-sm text-muted-foreground">
            {t("dashboard.noRecentRequests")}
          </p>
        )}
      </div>
      <Link
        className="mt-4 inline-flex min-h-10 items-center text-sm font-semibold text-primary"
        to={getAdminRouteHref("accessLogs")}
      >
        {t("dashboard.viewAllRequests")} →
      </Link>
    </Panel>
  );
}

export function RequestDetailsDialog({
  item,
  onClose,
}: {
  item: AccessLog | null;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const detailQuery = useQuery({
    queryKey: ["admin", "log-detail", item?.id],
    queryFn: ({ signal }) =>
      adminApi
        .getLogDetail(item!.id, { signal })
        .then((response) => response.data),
    enabled: item !== null,
    retry: false,
  });
  const detail = detailQuery.data as AccessLogDetail | undefined;
  const source = detail ?? item;
  return (
    <Modal
      open={item !== null}
      onClose={onClose}
      title={t("dashboard.requestDetails")}
      width={820}
    >
      <div className="grid gap-4 text-sm sm:grid-cols-2">
        {source && (
          <>
            <div>
              <p className="text-muted-foreground">{t("dashboard.package")}</p>
              <p className="mt-1 font-medium">{source.package_name || "—"}</p>
            </div>
            <div>
              <p className="text-muted-foreground">
                {t("dashboard.ecosystem")}
              </p>
              <p className="mt-1">{source.adapter_type || "—"}</p>
            </div>
            <div>
              <p className="text-muted-foreground">{t("dashboard.result")}</p>
              <p className="mt-1">
                {source.cache_result ||
                  (source.hit
                    ? t("dashboard.cacheHit")
                    : t("dashboard.unknown"))}
              </p>
            </div>
            <div>
              <p className="text-muted-foreground">{t("dashboard.upstream")}</p>
              <p className="mt-1">{source.upstream || "—"}</p>
            </div>
            <div>
              <p className="text-muted-foreground">{t("dashboard.latency")}</p>
              <p className="mt-1 font-mono tabular-nums">
                {source.latency_ms} ms
              </p>
            </div>
            <div>
              <p className="text-muted-foreground">
                {t("dashboard.responseSize")}
              </p>
              <p className="mt-1 font-mono tabular-nums">
                {formatBytes(source.bytes_sent)}
              </p>
            </div>
          </>
        )}
        {detailQuery.isPending && (
          <p className="text-muted-foreground sm:col-span-2">
            {t("loading")}
          </p>
        )}
        {detailQuery.isError && (
          <p className="text-warning sm:col-span-2">
            {t("dashboard.detailUnavailable")}
          </p>
        )}
      </div>
    </Modal>
  );
}

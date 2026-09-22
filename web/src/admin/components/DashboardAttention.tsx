import {
  ChevronRight,
  CircleCheck,
  HardDrive,
  TriangleAlert,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { useTranslation } from "react-i18next";
import { useState } from "react";
import { Link } from "react-router";

import { getAdminRouteHref } from "@/admin/routes";
import ButtonV2 from "@/components/app/button";
import Icon from "@/components/app/icon";
import type { LucideIcon } from "lucide-react";
import QueryErrorState from "@/components/app/error-state";
import type {
  DashboardIssueCategory,
  ObservedStatus,
} from "@/admin/dashboardStatus";
import type { DashboardUpstream } from "@/lib/adminApi.types";
import { upstreamStatus } from "@/lib/upstreamStatus";
import Modal from "@/components/app/modal";

interface DashboardAttentionProps {
  issues: DashboardIssueCategory[];
  policyState?: ObservedStatus;
  policyRefreshing: boolean;
  onRetryPolicy: () => void;
  isPending: boolean;
  isFetching: boolean;
  initialErrorMessage?: string;
  isStale: boolean;
  upstreams: DashboardUpstream[];
  cacheUsagePercent?: number;
  onRetry: () => void;
}

interface AttentionItemProps {
  icon: LucideIcon;
  title: string;
  detail: string;
  tone: "destructive" | "warning";
  action: string;
  onInspect?: () => void;
}

function AttentionItem({
  icon,
  title,
  detail,
  tone,
  action,
  onInspect,
}: AttentionItemProps) {
  // The attention tone is the shared vocabulary's, not a local colour pick:
  // a refusal or a failure outranks a degraded or partial result.
  const toneClass =
    tone === "destructive"
      ? "bg-destructive/10 text-destructive"
      : "bg-warning/10 text-warning";

  return (
    <li className="dashboard-attention-item min-w-0 py-1 first:pt-0 last:pb-0">
      <button
        type="button"
        onClick={onInspect}
        aria-label={action}
        className="group flex min-h-12 w-full min-w-0 items-center gap-3 rounded-sm px-2 py-1.5 text-left no-underline transition-colors duration-150 hover:bg-card"
      >
        <span
          aria-hidden
          className={`inline-flex size-7 shrink-0 items-center justify-center rounded-md ${toneClass}`}
        >
          <Icon icon={icon} size="sm" />
        </span>
        <div className="min-w-0 flex-1">
          <h3 className="text-label font-semibold text-foreground">{title}</h3>
          <p className="mt-1 text-meta leading-[1.55] text-muted-foreground">
            {detail}
          </p>
        </div>
        <span aria-hidden="true" className="shrink-0 text-primary">
          <ChevronRight className="icon icon-sm" aria-hidden="true" />
        </span>
      </button>
    </li>
  );
}

export default function DashboardAttention({
  issues,
  policyState,
  policyRefreshing,
  onRetryPolicy,
  isPending,
  isFetching,
  initialErrorMessage,
  isStale,
  upstreams,
  cacheUsagePercent,
  onRetry,
}: DashboardAttentionProps) {
  const { t } = useTranslation();
  const [selectedIssue, setSelectedIssue] = useState<
    "upstream" | "cache" | "policy" | null
  >(null);
  const cacheNeedsAttention = issues.includes("cache");
  const policyNeedsAttention = issues.includes("policy");
  const issueCount = issues.length;
  const hasIssues = issueCount > 0;
  const upstreamNames = upstreams
    .slice(0, 3)
    .map((item) => item.name)
    .join(t("dashboard.listSeparator"));
  const issueItems = [
    upstreams.length > 0 ? "upstream" : null,
    cacheNeedsAttention ? "cache" : null,
    policyNeedsAttention ? "policy" : null,
  ].filter((item): item is "upstream" | "cache" | "policy" => item !== null);
  const visibleItems = issueItems.slice(0, 2);

  return (
    <section
      data-dashboard-panel
      data-dashboard-attention-panel
      aria-labelledby="dashboard-attention-title"
      aria-describedby="dashboard-attention-description"
      aria-busy={isPending || undefined}
      className="dashboard-attention-panel min-w-0 overflow-hidden rounded-lg border border-border bg-card"
    >
      <header className="flex min-h-12 items-center justify-between gap-3 border-b border-border px-4 py-2">
        <div className="min-w-0">
          <h2
            id="dashboard-attention-title"
            className="text-body font-semibold text-foreground"
          >
            {t("dashboard.needsAttention")}
          </h2>
          <p id="dashboard-attention-description" className="sr-only">
            {t("dashboard.attentionHint")}
          </p>
        </div>
        {!isPending && !initialErrorMessage && (
          <span
            className={cn(
              "shrink-0 text-meta tabular-nums",
              hasIssues ? "text-warning" : "text-muted-foreground",
            )}
            aria-label={t("dashboard.attentionCount", { count: issueCount })}
          >
            {t("dashboard.attentionCategories", { count: issueCount })}
          </span>
        )}
      </header>

      {isPending ? (
        <div aria-hidden="true" className="space-y-3 p-4">
          <div className="h-16 animate-pulse rounded-sm bg-muted" />
          <div className="h-12 animate-pulse rounded-sm bg-muted" />
        </div>
      ) : initialErrorMessage ? (
        <div className="dashboard-attention-body">
          <QueryErrorState message={initialErrorMessage} onRetry={onRetry} />
        </div>
      ) : (
        <div className="p-4">
          {isStale && (
            <div
              role="status"
              className="mb-3 flex flex-wrap items-center justify-between gap-2 rounded-sm bg-warning/10 px-3 py-2 text-meta text-warning"
            >
              <span>{t("attention.queueStale")}</span>
              <ButtonV2
                type="button"
                variant="secondary"
                size="sm"
                aria-busy={isFetching || undefined}
                disabled={isFetching}
                onClick={onRetry}
              >
                {t("attention.retry")}
              </ButtonV2>
            </div>
          )}

          {visibleItems.length > 0 ? (
            <ul className="divide-y divide-border">
              {visibleItems.includes("upstream") && (
                <AttentionItem
                  icon={TriangleAlert}
                  title={t("attention.upstreamsTitle")}
                  detail={t("dashboard.upstreamWarning", {
                    count: upstreams.length,
                    names: upstreamNames,
                  })}
                  tone={
                    upstreams.some((item) => upstreamStatus(item) === "failed")
                      ? "destructive"
                      : "warning"
                  }
                  action={t("dashboard.viewUpstreams")}
                  onInspect={() => setSelectedIssue("upstream")}
                />
              )}
              {visibleItems.includes("cache") && (
                <AttentionItem
                  icon={HardDrive}
                  title={t("attention.cacheTitle")}
                  detail={t("dashboard.storageWarning", {
                    percent: cacheUsagePercent?.toFixed(1),
                  })}
                  tone={
                    (cacheUsagePercent ?? 0) > 95 ? "destructive" : "warning"
                  }
                  action={t("dashboard.manageCache")}
                  onInspect={() => setSelectedIssue("cache")}
                />
              )}
              {visibleItems.includes("policy") && (
                <AttentionItem
                  icon={TriangleAlert}
                  title={t(
                    policyState === "degraded"
                      ? "dashboard.policyDegraded"
                      : "dashboard.policyUnknown",
                  )}
                  detail={t("dashboard.policyAttentionHint")}
                  tone="warning"
                  action={t("dashboard.refreshPolicy")}
                  onInspect={() => setSelectedIssue("policy")}
                />
              )}
            </ul>
          ) : policyState !== undefined ? (
            <div
              role="status"
              className="flex items-center gap-3 py-2"
            >
              <span
                aria-hidden
                className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-sm text-muted-foreground"
              >
                <CircleCheck className="icon icon-sm" aria-hidden="true" />
              </span>
              <div className="min-w-0">
                <h3 className="text-body font-semibold text-foreground">
                  {t("dashboard.noActiveIssues")}
                </h3>
                <p className="mt-1.5 text-label leading-[1.55] text-muted-foreground">
                  {t("dashboard.noActiveIssuesHint")}
                </p>
              </div>
            </div>
          ) : null}
        </div>
      )}
      <Modal
        open={selectedIssue !== null}
        onClose={() => setSelectedIssue(null)}
        title={t("dashboard.needsAttention")}
        width={560}
      >
        <div className="space-y-3 text-sm leading-5">
          <p>
            {selectedIssue === "upstream"
              ? t("attention.upstreamsDetail", {
                  count: upstreams.length,
                  names: upstreamNames,
                })
              : selectedIssue === "cache"
                ? t("attention.cacheDetail", {
                    percent: cacheUsagePercent?.toFixed(1),
                  })
                : t("dashboard.policyAttentionHint")}
          </p>
          {selectedIssue === "policy" ? (
            <ButtonV2
              variant="secondary"
              size="sm"
              disabled={policyRefreshing}
              aria-busy={policyRefreshing || undefined}
              onClick={onRetryPolicy}
            >
              {t("dashboard.refreshPolicy")}
            </ButtonV2>
          ) : (
            <Link
              className="inline-flex min-h-10 items-center font-semibold text-primary"
              to={
                selectedIssue === "upstream"
                  ? getAdminRouteHref("upstreams")
                  : getAdminRouteHref("cache")
              }
              onClick={() => setSelectedIssue(null)}
            >
              {selectedIssue === "upstream"
                ? t("attention.reviewUpstreams")
                : t("attention.manageCache")} {" "}
              →
            </Link>
          )}
        </div>
      </Modal>
    </section>
  );
}

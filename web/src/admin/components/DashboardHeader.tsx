import { type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import Button, { LinkButton } from "@/components/app/button";
import Modal from "@/components/app/modal";
import { getAdminRouteHref } from "@/admin/routes";
import type { dashboardStatus, ObservedStatus } from "@/admin/dashboardStatus";

interface Props {
  upstreamHealth: ReturnType<typeof dashboardStatus>["upstreamHealth"];
  policyState: ObservedStatus;
  proxyResponding: boolean;
  snapshotUpdatedAt: number;
  policyUpdatedAt: number;
  proxyUpdatedAt: number;
  snapshotError: boolean;
  policyError: boolean;
  proxyError: boolean;
  uptimeSeconds?: number;
  refreshing: boolean;
  onRefresh: () => void;
  updatedAt?: string;
  periodControl?: ReactNode;
  statusDetailsOpen: boolean;
  onStatusDetailsChange: (open: boolean) => void;
}

export default function DashboardHeader(props: Props) {
  const { t, i18n } = useTranslation();
  const { upstreamHealth, policyState, proxyResponding } = props;
  const proxyLabel = t(
    proxyResponding ? "dashboard.proxyAvailable" : "dashboard.proxyUnknown",
  );
  const policyLabel = t(
    policyState === "healthy"
      ? "dashboard.policyHealthy"
      : policyState === "degraded"
        ? "dashboard.policyDegraded"
        : "dashboard.policyUnknown",
  );
  const upstreamLabel = !upstreamHealth
    ? t("dashboard.upstreamStatusUnknown")
    : !upstreamHealth.total
      ? t("dashboard.noUpstreams")
      : upstreamHealth.slow || upstreamHealth.failed
        ? t("dashboard.upstreamIssues", upstreamHealth)
        : t("dashboard.upstreamOperational", upstreamHealth);
  const time = (value: number) =>
    value ? new Date(value).toLocaleString(i18n.language) : "—";
  const runtimeUptime = (seconds?: number) => {
    if (seconds == null) return "—";
    if (seconds < 60) return t("dashboard.uptimeUnderMinute");
    if (seconds < 3600)
      return t("dashboard.uptimeMinutes", {
        count: Math.floor(seconds / 60),
      });
    if (seconds < 86400)
      return t("dashboard.uptimeHours", {
        count: Math.floor(seconds / 3600),
      });
    const days = Math.floor(seconds / 86400);
    const hours = Math.floor(seconds / 3600) % 24;
    return hours
      ? `${t("dashboard.uptimeDays", { count: days })} ${t("dashboard.uptimeHours", { count: hours })}`
      : t("dashboard.uptimeDays", { count: days });
  };

  return (
    <header data-dashboard-status className="space-y-2">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div data-dashboard-heading className="min-w-0">
          <h1 className="font-semibold">{t("dashboard.overviewTitle")}</h1>
          <p className="text-sm text-muted-foreground">
            {t("dashboard.overviewSubtitle")}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          {props.updatedAt && (
            <span className="self-center text-sm text-muted-foreground">
              {t("dashboard.updatedAt", { time: props.updatedAt })}
            </span>
          )}
          {props.periodControl}
          <Button
            variant="secondary"
            size="sm"
            onClick={props.onRefresh}
            disabled={props.refreshing}
            aria-busy={props.refreshing || undefined}
            title={t("dashboard.refreshScope")}
          >
            {t("dashboard.refreshStatus")}
          </Button>
          <LinkButton
            variant="secondary"
            size="sm"
            to={getAdminRouteHref("connect")}
          >
            {t("dashboard.connectClient")}
          </LinkButton>
        </div>
      </div>

      <Modal
        open={props.statusDetailsOpen}
        onClose={() => props.onStatusDetailsChange(false)}
        title={t("dashboard.statusDetails")}
        width={560}
      >
        <div className="space-y-4 text-sm leading-5">
          <section>
            <h3 className="font-semibold">{proxyLabel}</h3>
            <p className="mt-1 text-muted-foreground">
              {t("dashboard.proxyScope")}
            </p>
            <p className="text-muted-foreground">
              {t("dashboard.fetchedAt", {
                time: time(props.proxyUpdatedAt),
                seconds: 5,
              })}
            </p>
            {props.proxyError && <p>{t("dashboard.liveUnavailable")}</p>}
          </section>
          <section>
            <h3 className="font-semibold">{t("dashboard.runtimeStatus")}</h3>
            <p className="text-muted-foreground">
              {runtimeUptime(props.uptimeSeconds)}
            </p>
          </section>
          <section>
            <h3 className="font-semibold">{policyLabel}</h3>
            <p className="mt-1 text-muted-foreground">
              {t("dashboard.fetchedAt", {
                time: time(props.policyUpdatedAt),
                seconds: 30,
              })}
            </p>
            {props.policyError && <p>{t("dashboard.policyRefreshIssue")}</p>}
          </section>
          <section>
            <h3 className="font-semibold">
              {upstreamHealth
                ? t("dashboard.upstreamSummary", upstreamHealth)
                : upstreamLabel}
            </h3>
            <p className="mt-1 text-muted-foreground">
              {t("dashboard.upstreamScope")}
            </p>
            <p className="text-muted-foreground">
              {t("dashboard.fetchedAt", {
                time: time(props.snapshotUpdatedAt),
                seconds: 30,
              })}
            </p>
            {props.snapshotError && (
              <p>
                {t(
                  props.snapshotUpdatedAt
                    ? "dashboard.snapshotStale"
                    : "dashboard.snapshotUnavailable",
                )}
              </p>
            )}
          </section>
          <p className="text-muted-foreground">{t("dashboard.refreshScope")}</p>
        </div>
      </Modal>
    </header>
  );
}

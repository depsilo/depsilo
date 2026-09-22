/**
 * The authenticated Admin shell: a persistent workspace rail, a quiet utility
 * bar, and one content outlet. Stage A keeps the shell's geometry (a 232px
 * rail, a contextual topbar) and rebuilds it on semantic tokens, so Admin and Portal
 * share one palette and differ only by layout.
 */
import { ChevronRight, LogOut, Menu, Settings } from "lucide-react";
import { type RefObject, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Outlet, useLocation, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";

import Drawer from "@/components/app/drawer";
import Icon from "@/components/app/icon";
import type { LucideIcon } from "lucide-react";
import LanguageToggle from "@/components/app/language-toggle";
import Logo from "@/components/app/logo";
import ThemeToggle from "@/components/app/theme-toggle";
import { usePrincipal } from "@/hooks/usePrincipal";
import { authApi, statsApi } from "@/lib/api";
import { removeLocalStorage } from "@/lib/storage";
import { cn, formatVersion } from "@/lib/utils";
import { adminNavigationGroups, resolveAdminRoute } from "../routes";

const RAIL_WIDTH = "232px";

interface NavSection {
  id: string;
  label: string;
  icon: LucideIcon;
  href: string;
  active: boolean;
  current: boolean;
}

interface SidebarContentProps {
  sections: NavSection[];
  surface: "sidebar" | "drawer";
  /** True when the current route belongs to the instance-management group. */
  instanceActive: boolean;
  username?: string;
  canWrite: boolean;
  version?: string;
  firstNavigationRef?: RefObject<HTMLAnchorElement | null>;
  reserveCloseSpace?: boolean;
  onNavigate?: () => void;
  onLogout: () => void;
}

function SidebarContent({
  sections,
  surface,
  instanceActive,
  username,
  canWrite,
  version,
  firstNavigationRef,
  reserveCloseSpace = false,
  onNavigate,
  onLogout,
}: SidebarContentProps) {
  const { t } = useTranslation();
  const preferredFocusSectionId =
    sections.find((section) => section.active)?.id ?? sections[0]?.id;

  return (
    <>
      <div
        data-admin-sidebar-header
        className={cn(
          "flex shrink-0 items-center gap-2.5 py-5 pl-5",
          reserveCloseSpace ? "pr-16" : "pr-5",
        )}
      >
        <Link
          data-admin-brand-link
          to="/"
          onClick={onNavigate}
          aria-label={t("portal.backLink")}
          title={t("portal.backLink")}
          className="flex min-w-0 items-center gap-2.5 rounded-md text-foreground no-underline transition-opacity hover:opacity-75"
        >
          <Logo size={26} />
          <span className="text-title font-bold">Depsilo</span>
        </Link>
        <span
          className="ml-auto inline-flex min-w-16 max-w-[76px] items-center justify-center truncate rounded-sm bg-sidebar-accent px-1.5 py-0.5 font-mono text-meta tabular-nums text-muted-foreground"
          title={version}
        >
          {formatVersion(version)}
        </span>
      </div>

      <nav
        data-admin-nav-scroll
        data-admin-nav-surface={surface}
        aria-label={t("nav.adminNavigation")}
        className="min-h-0 flex-1 overflow-y-auto overscroll-contain py-2"
      >
        <div className="space-y-0.5 px-2.5">
          {sections.map((section) => (
            <div
              key={section.id}
              data-admin-nav-group={section.id}
              data-admin-nav-active={section.active ? "true" : "false"}
            >
              <div
                data-admin-workspace-row
                data-admin-workspace-current={
                  section.active ? "true" : undefined
                }
                className={cn(
                  "relative flex min-w-0 items-center rounded-md transition-colors",
                  section.active
                    ? "bg-sidebar-accent"
                    : "hover:bg-sidebar-accent",
                )}
              >
                {/* The active workspace is marked by one brand rule at the rail's
                    own edge rather than by tinting the whole row: the accent is
                    for marking position, and a tinted row would spend it six
                    times over in a rail this short. */}
                {section.active && (
                  <span
                    aria-hidden="true"
                    className="absolute inset-y-2 left-0 w-0.5 rounded-full bg-sidebar-primary"
                  />
                )}
                <Link
                  ref={
                    section.id === preferredFocusSectionId
                      ? firstNavigationRef
                      : undefined
                  }
                  to={section.href}
                  onClick={onNavigate}
                  aria-current={
                    section.active
                      ? section.current
                        ? "page"
                        : "location"
                      : undefined
                  }
                  className={cn(
                    "flex min-h-10 min-w-0 flex-1 items-center gap-2.5 rounded-md px-2.5 py-2 text-body no-underline",
                    section.active
                      ? "font-semibold text-foreground"
                      : "font-medium text-muted-foreground hover:text-foreground",
                  )}
                >
                  <Icon icon={section.icon} size="sm" className="shrink-0" />
                  <span className="min-w-0 flex-1 truncate">
                    {section.label}
                  </span>
                </Link>
              </div>
            </div>
          ))}
        </div>
      </nav>

      <div
        data-admin-sidebar-preferences
        className="flex shrink-0 items-center gap-1 border-t border-sidebar-border px-3 py-2"
        aria-label={t("nav.preferences")}
      >
        <LanguageToggle variant="admin" />
        <ThemeToggle labeled variant="admin" />
      </div>

      <div
        data-admin-sidebar-footer
        className="shrink-0 border-t border-sidebar-border px-3 py-3"
      >
        <Link
          to="/admin/users"
          onClick={onNavigate}
          aria-label={t("nav.instanceManagement")}
          aria-current={instanceActive ? "page" : undefined}
          className={cn(
            "relative mb-2 flex min-h-10 items-center gap-2.5 rounded-md px-2 py-2 text-body no-underline transition-colors",
            instanceActive
              ? "bg-sidebar-accent font-semibold text-foreground"
              : "font-medium text-muted-foreground hover:bg-sidebar-accent hover:text-foreground",
          )}
        >
          {instanceActive && (
            <span
              aria-hidden="true"
              className="absolute inset-y-2 left-0 w-0.5 rounded-full bg-sidebar-primary"
            />
          )}
          <Settings className="icon icon-sm" aria-hidden="true" />
          <span>{t("nav.instanceManagement")}</span>
        </Link>
        <div className="group flex cursor-default items-center gap-2.5 rounded-md px-2 py-2 transition-colors hover:bg-sidebar-accent">
          <div className="flex size-8 shrink-0 items-center justify-center rounded-sm bg-primary text-body font-semibold text-primary-foreground">
            {username?.[0]?.toUpperCase() || "A"}
          </div>
          <div className="min-w-0 flex-1">
            <p className="truncate text-body leading-tight font-medium text-foreground">
              {username}
            </p>
            <p className="mt-0.5 text-meta leading-tight text-muted-foreground">
              {canWrite ? t("nav.admin") : t("nav.readonly")}
            </p>
          </div>
          {/* Keyboard users must see this control even though pointing devices
              only reveal it on row hover. */}
          <button
            type="button"
            onClick={onLogout}
            className="inline-flex min-h-10 min-w-10 cursor-pointer items-center justify-center rounded-sm bg-transparent p-1.5 text-muted-foreground opacity-100 transition-[opacity,color] hover:text-foreground focus-visible:opacity-100 lg:opacity-0 lg:group-hover:opacity-100 lg:group-focus-within:opacity-100"
            aria-label={t("nav.logout")}
          >
            <LogOut className="icon icon-sm" aria-hidden="true" />
          </button>
        </div>
      </div>
    </>
  );
}

export default function AdminShellLayout() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  const firstMobileNavigationRef = useRef<HTMLAnchorElement>(null);
  const { principal, canWrite } = usePrincipal();
  const activeRoute = resolveAdminRoute(location.pathname);
  const isOverview =
    activeRoute?.id === "dashboard" || activeRoute?.id === "bandwidth";
  const isDashboard = activeRoute?.id === "dashboard";
  const { data: stats } = useQuery<{
    service: { version: string; status: string };
  }>({
    queryKey: ["stats-status"],
    queryFn: async ({ signal }) => (await statsApi.getStats({ signal })).data,
    refetchInterval: 30000,
    staleTime: 30000,
  });

  const sections: NavSection[] = adminNavigationGroups
    .filter((group) => !group.hiddenFromSidebar)
    .map((group) => ({
      id: group.id,
      label: t(group.titleKey),
      icon: group.icon,
      href: group.href,
      active: activeRoute?.navGroup === group.id,
      current: activeRoute?.href === group.href,
    }));
  const pageTitle = activeRoute ? t(activeRoute.titleKey) : t("notFound.title");
  const activeSection = sections.find((section) => section.active);
  const showPageBreadcrumb =
    !activeSection || activeSection.label !== pageTitle;
  const sidebarProps = {
    sections,
    instanceActive: activeRoute?.navGroup === "instance",
    username: principal?.username,
    canWrite,
    version: stats?.service?.version,
  };

  const handleLogout = async () => {
    try {
      await authApi.logout();
    } catch {
      /* logout stays local when the server is unreachable */
    }
    removeLocalStorage("token");
    queryClient.clear();
    navigate("/admin/login", { replace: true });
  };

  return (
    <div data-admin-shell className="relative flex min-h-screen bg-background">
      <aside
        className="fixed inset-y-0 left-0 z-30 hidden flex-col border-r border-sidebar-border bg-sidebar lg:flex"
        style={{ width: RAIL_WIDTH }}
      >
        <SidebarContent
          {...sidebarProps}
          surface="sidebar"
          onLogout={() => {
            void handleLogout();
          }}
        />
      </aside>

      <Drawer
        open={mobileNavOpen}
        onOpenChange={setMobileNavOpen}
        title={t("nav.adminNavigation")}
        initialFocus={firstMobileNavigationRef}
      >
        <div
          id="admin-mobile-navigation"
          className="flex h-full min-h-0 flex-col overflow-hidden bg-sidebar"
        >
          <SidebarContent
            {...sidebarProps}
            surface="drawer"
            firstNavigationRef={firstMobileNavigationRef}
            reserveCloseSpace
            onNavigate={() => setMobileNavOpen(false)}
            onLogout={() => {
              void handleLogout();
            }}
          />
        </div>
      </Drawer>

      <div
        data-admin-main
        className={cn(
          "min-w-0 flex-1 lg:ml-[232px]",
          isDashboard ? "bg-[#f3f6fa] dark:bg-[#0d1727]" : "bg-background",
        )}
      >
        <header
          data-admin-topbar
          className={cn(
            "fixed inset-x-0 top-0 z-20 flex items-center gap-x-2.5 border-b border-border bg-background px-4 sm:px-6 lg:left-[232px] lg:px-8",
            "h-12",
            isOverview && "lg:hidden",
          )}
        >
          <button
            type="button"
            className="inline-flex size-10 shrink-0 items-center justify-center rounded-md bg-transparent text-muted-foreground transition-colors hover:bg-accent hover:text-foreground lg:hidden"
            onClick={() => setMobileNavOpen(true)}
            aria-label={t("nav.openNavigation")}
            aria-expanded={mobileNavOpen}
            aria-controls="admin-mobile-navigation"
          >
            <Menu className="icon icon-sm" aria-hidden="true" />
          </button>
          <div data-admin-breadcrumb className="min-w-0 flex-1">
            <div
              className={cn(
                "min-w-0 items-center gap-1.5 text-label font-medium",
                showPageBreadcrumb ? "flex" : "flex lg:hidden",
              )}
            >
              {activeSection && (
                <span
                  className={cn(
                    "truncate",
                    showPageBreadcrumb
                      ? "text-muted-foreground"
                      : "text-foreground",
                  )}
                >
                  {activeSection.label}
                </span>
              )}
              {showPageBreadcrumb && (
                <>
                  {activeSection && (
                    <span aria-hidden="true" className="text-muted-foreground">
                      <ChevronRight
                        className="icon icon-sm"
                        aria-hidden="true"
                      />
                    </span>
                  )}
                  <span className="truncate text-foreground">{pageTitle}</span>
                </>
              )}
            </div>
          </div>
        </header>

        <main
          className={cn(
            "min-h-screen pb-6",
            isDashboard ? "bg-[#f3f6fa] dark:bg-[#0d1727]" : "bg-background",
            isOverview ? "pt-16 lg:pt-5" : "pt-16",
          )}
        >
          {/* One width cap for every Admin surface, owned here so that a
              banner rendered above the page frame still aligns with the page
              below it. `readable` pages cap themselves narrower again. */}
          <div
            data-admin-outlet
            className="mx-auto w-full max-w-[1840px] px-4 sm:px-6 lg:px-8"
          >
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  );
}

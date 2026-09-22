import {
  test,
  expect,
  mockAdminApi,
  setUiPreferences,
} from "./fixtures/admin-api";

test("overview renders resource and traffic sections and opens a centered request dialog", async ({
  page,
}) => {
  await setUiPreferences(page, "light", "en");
  const snapshot = {
    last_24h: {
      total_requests: 12,
      hit_count: 8,
      hit_rate: 2 / 3,
      bytes_served: 2048,
      avg_latency_ms: 42,
    },
    prev_24h: {
      total_requests: 10,
      hit_rate: 0.5,
      bytes_served: 1024,
      avg_latency_ms: 50,
    },
    upstreams: [
      {
        id: 1,
        name: "pypi",
        adapter: "pypi",
        healthy: true,
        avg_latency_ms: 42,
        success_rate: 1,
      },
    ],
    daily_stats: [],
    top_packages: {},
    cache_usage_percent: 12,
    runtime: {
      heap_alloc_bytes: 4096,
      heap_sys_bytes: 8192,
      goroutines: 4,
      sampled_at: new Date().toISOString(),
      cpu: {
        state: "ready",
        percent: 3.2,
        window_seconds: 5,
        sampled_at: new Date().toISOString(),
        scope: "process",
        basis: "single_core",
      },
      memory: {
        state: "ready",
        rss_bytes: 10_485_760,
        sampled_at: new Date().toISOString(),
        scope: "process",
      },
    },
    cache_usage: {
      state: "ready",
      used_bytes: 1_073_741_824,
      quota_bytes: 8_589_934_592,
      sampled_at: new Date().toISOString(),
      basis: "logical_inventory",
    },
  };
  const request = {
    id: 8,
    adapter_type: "pypi",
    method: "GET",
    cache_key: "x",
    package_name: "idna",
    hit: true,
    cache_result: "hit",
    upstream: "pypi.org",
    latency_ms: 42,
    status_code: 200,
    client_ip: "127.0.0.1",
    bytes_sent: 2048,
    created_at: new Date().toISOString(),
  };
  await mockAdminApi(page, {
    "GET /api/v1/admin/dashboard": snapshot,
    "GET /api/v1/admin/dashboard/trends": {
      points: [
        {
          bucket: 1785600000,
          date: "2026-09-01",
          requests: 3,
          hits: 2,
          misses: 1,
          hit_rate: 2 / 3,
          bytes_served: 512,
          bytes_hit: 256,
          bytes_miss: 256,
          sum_latency_ms: 126,
          avg_latency_ms: 42,
          errors: 0,
        },
        {
          bucket: 1785686400,
          date: "2026-09-02",
          requests: 4,
          hits: 3,
          misses: 1,
          hit_rate: 0.75,
          bytes_served: 768,
          bytes_hit: 512,
          bytes_miss: 256,
          sum_latency_ms: 172,
          avg_latency_ms: 43,
          errors: 0,
        },
        {
          bucket: 1785772800,
          date: "2026-09-03",
          requests: 5,
          hits: 3,
          misses: 2,
          hit_rate: 0.6,
          bytes_served: 768,
          bytes_hit: 256,
          bytes_miss: 512,
          sum_latency_ms: 235,
          avg_latency_ms: 47,
          errors: 1,
        },
      ],
    },
    "GET /api/v1/admin/policy/status": {
      status: "healthy",
      using_stale_snapshot: false,
      snapshot_loaded_at: null,
      refresh_failures: 0,
      on_load_error: "",
    },
    "GET /api/v1/now": {
      status: "healthy",
      uptime_seconds: 3600,
      now_unix: 1,
      rate: {
        requests_per_min: 2,
        upstream_requests_per_min: 1,
        requests_per_second: 2 / 60,
        upstream_requests_per_second: 1 / 60,
        egress_bps: 32,
        ingress_bps: 0,
        has_data: true,
        state: "ready",
        coverage_seconds: 60,
        upstream_state: "ready",
        upstream_coverage_seconds: 60,
      },
      upstreams: { healthy: 1, total: 1 },
      sparkline: [
        { t: 1, requests: 1, hits: 1 },
        { t: 2, requests: 3, hits: 2 },
        { t: 3, requests: 2, hits: 1 },
      ],
      last_activity: {
        seconds_ago: 20,
        adapter_type: "pypi",
        hit: true,
        package_name: "idna",
      },
    },
    "GET /api/v1/admin/logs": {
      items: [request],
      total: 1,
      page: 1,
      page_size: 5,
    },
    "GET /api/v1/admin/bandwidth": {
      range: { start: "2026-01-01", end: "2026-01-30" },
      summary: {
        total_bytes: 2048,
        hit_bytes: 1024,
        miss_bytes: 1024,
        savings_rate: 0.5,
        total_requests: 12,
        hit_requests: 8,
        miss_requests: 4,
        time_saved_ms: 0,
        avg_hit_latency: 20,
        avg_miss_latency: 60,
      },
      daily: [],
      by_ecosystem: [],
      top_packages: [],
      by_upstream: [],
    },
    "GET /api/v1/admin/logs/8": {
      ...request,
      request_id: "req-8",
      cache_reason: "",
      policy_decision: "allow",
      policy_reason: "",
      delivery_result: "completed",
      delivery_reason: "",
      audit_events: [],
    },
  });
  await page.goto("/admin");
  await expect(page.getByText("Runtime resources")).toBeVisible();
  await expect(page.getByText("Traffic overview")).toBeVisible();
  await expect(page.locator(".dashboard-tone-cpu").first()).toHaveCSS(
    "color",
    "rgb(22, 163, 101)",
  );
  await expect(page.locator(".dashboard-tone-cache").first()).toHaveCSS(
    "color",
    "rgb(128, 85, 222)",
  );
  await expect(page.locator(".dashboard-tone-upstream").first()).toHaveCSS(
    "color",
    "rgb(216, 132, 22)",
  );
  await expect(page.locator(".dashboard-trend-service").first()).toHaveCSS(
    "color",
    "rgb(37, 99, 235)",
  );
  await expect(page.locator(".dashboard-resource-sparkline path")).toHaveCount(
    1,
  );
  await page.screenshot({
    path: "/tmp/depsilo-dashboard-closed.png",
    fullPage: true,
  });
  await page.getByRole("row").nth(1).click();
  await expect(page.getByRole("dialog")).toContainText("Request details");
  await expect(page.getByRole("dialog")).toContainText("idna");
  await page.screenshot({
    path: "/tmp/depsilo-dashboard-request-dialog.png",
    fullPage: true,
  });
});

import {
  test,
  expect,
  mockAdminApi,
  setUiPreferences,
} from "./fixtures/admin-api";

const upstreams = Array.from({ length: 25 }, (_, i) => ({
  id: i + 1,
  name: `source-${i + 1}`,
  adapter: "pypi",
  healthy: true,
  avg_latency_ms: i < 2 ? 50 : 240,
  success_rate: 1,
}));
const window = {
  total_requests: 100,
  hit_count: 80,
  hit_rate: 0.8,
  bytes_served: 102400,
  avg_latency_ms: 40,
};
const snapshot = {
  last_24h: window,
  prev_24h: window,
  upstreams,
  daily_stats: [],
  top_packages: {},
  cache_usage_percent: 10,
};
const now = {
  status: "healthy",
  uptime_seconds: 600,
  now_unix: 1786032000,
  rate: {
    requests_per_min: 12,
    has_data: true,
    egress_bps: 100,
    ingress_bps: 20,
  },
  upstreams: { healthy: 25, total: 25 },
  sparkline: [],
};
const policy = {
  status: "unavailable",
  using_stale_snapshot: false,
  snapshot_loaded_at: null,
  refresh_failures: 0,
  on_load_error: "use_stale_then_allow",
};

test("one upstream snapshot drives header, flow and attention with one health mapping", async ({
  page,
}) => {
  await setUiPreferences(page, "light", "en");
  await mockAdminApi(page, {
    "GET /api/v1/admin/dashboard": snapshot,
    "GET /api/v1/now": now,
    "GET /api/v1/admin/policy/status": policy,
  });
  await page.goto("/admin");
  const flow = page.locator('[data-query-key="now"]');
  await expect(flow.getByRole("link")).toHaveText(/25 reachable upstreams/);
  await expect(
    page.locator('section[aria-labelledby="dashboard-attention-title"]'),
  ).toContainText("23 upstreams");
  await expect(page.locator("[data-dashboard-status-strip]")).toContainText(
    "Attention",
  );
  await page
    .locator("[data-dashboard-status-strip]")
    .locator('button[aria-haspopup="dialog"]')
    .click();
  await expect(page.getByRole("dialog")).toContainText("Policy: unknown");
});

test("zero requests keep real zeros while sample metrics stay empty", async ({
  page,
}) => {
  await setUiPreferences(page, "light", "en");
  await mockAdminApi(page, {
    "GET /api/v1/admin/dashboard": {
      last_24h: {
        total_requests: 0,
        hit_count: 0,
        hit_rate: 0,
        bytes_served: 0,
        avg_latency_ms: 0,
      },
      prev_24h: {
        total_requests: 0,
        hit_rate: 0,
        bytes_served: 0,
        avg_latency_ms: 0,
      },
      upstreams: [
        {
          id: 1,
          name: "source-1",
          adapter: "pypi",
          healthy: true,
          avg_latency_ms: 40,
          success_rate: 1,
        },
      ],
      daily_stats: [],
      top_packages: {},
      cache_usage_percent: 10,
    },
    "GET /api/v1/admin/dashboard/trends": { points: [] },
    "GET /api/v1/now": {
      status: "healthy",
      uptime_seconds: 60,
      now_unix: 1786032000,
      rate: {
        requests_per_min: 0,
        has_data: false,
        egress_bps: 0,
        ingress_bps: 0,
      },
      upstreams: { healthy: 1, total: 1 },
      sparkline: [],
    },
    "GET /api/v1/admin/policy/status": {
      status: "healthy",
      using_stale_snapshot: false,
      snapshot_loaded_at: null,
      refresh_failures: 0,
      on_load_error: "use_stale_then_allow",
    },
  });
  await page.goto("/admin");
  await expect(page.locator("[data-dashboard-kpis]")).toHaveCount(0);
  await expect(
    page.locator("[data-dashboard-traffic-card]").nth(2),
  ).toContainText(/0\s*B/);
  const panelText = await page
    .locator("[data-dashboard-panel]")
    .allTextContents();
  expect(panelText.every((text) => !text.includes("0 ms"))).toBe(true);
  await expect(page.locator(".dashboard-resource-sparkline")).toHaveCount(0);
  await expect(
    page.locator('[data-query-key="dashboard-trends"]'),
  ).toContainText("No traffic in this period (30d)");
});

test("a failed snapshot remains an error and does not become zero traffic", async ({
  page,
}) => {
  await setUiPreferences(page, "light", "en");
  await mockAdminApi(page, {
    "GET /api/v1/admin/dashboard": {
      status: 503,
      body: { message: "dashboard unavailable" },
    },
    "GET /api/v1/admin/dashboard/trends": { points: [] },
    "GET /api/v1/now": {
      status: "healthy",
      uptime_seconds: 60,
      now_unix: 1786032000,
      rate: {
        requests_per_min: 4,
        has_data: true,
        egress_bps: 0,
        ingress_bps: 0,
      },
      upstreams: { healthy: 0, total: 0 },
      sparkline: [],
    },
    "GET /api/v1/admin/policy/status": {
      status: "healthy",
      using_stale_snapshot: false,
      snapshot_loaded_at: null,
      refresh_failures: 0,
      on_load_error: "use_stale_then_allow",
    },
  });
  await page.goto("/admin");
  await expect(page.locator("[data-dashboard-status-strip]")).toContainText(
    "Unknown",
  );
  await page.locator('[data-dashboard-status-strip] button[aria-haspopup="dialog"]').click();
  await expect(page.getByRole("dialog")).toContainText("Snapshot unavailable");
  await expect(page.locator("[data-dashboard-kpis]")).toHaveCount(0);
  const panelText = await page
    .locator("[data-dashboard-panel]")
    .allTextContents();
  expect(panelText.every((text) => !text.includes("0 ms"))).toBe(true);
});

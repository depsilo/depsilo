import AxeBuilder from "@axe-core/playwright";
import {
  expect,
  expectResolvedUiPreferences,
  mockAdminApi,
  resolvedTokenColor,
  setUiPreferences,
  test,
} from "./fixtures/admin-api";

const populatedTrendPoints = [0, 1, 2, 3].map((index) => {
  const requests = 12 + index;
  const hits = 8 + index;
  const misses = requests - hits;
  return {
    bucket: 1783641600 + index * 300,
    date: "2026-07-10",
    requests,
    hits,
    misses,
    hit_rate: hits / requests,
    bytes_served: requests * 128,
    bytes_hit: hits * 128,
    bytes_miss: misses * 128,
    sum_latency_ms: requests * (10 + index),
    avg_latency_ms: 10 + index,
    errors: index % 2,
  };
});

/**
 * Recharts ships in its own lazy chunk. Under parallel load its fetch and parse
 * can outlast the default expect timeout, which would fail the suite for a
 * reason unrelated to the chart contract, so the chart gets an explicit budget.
 */
const CHART_TIMEOUT = 20_000;

async function trendChart(page: import("@playwright/test").Page) {
  const chart = page.locator(
    '[data-query-key="dashboard-trends"] .recharts-wrapper',
  );
  await expect(chart).toBeVisible({ timeout: CHART_TIMEOUT });
  return chart;
}

test("light theme admin chrome has no color-contrast violations", async ({
  page,
}) => {
  await setUiPreferences(page, "light", "zh");
  await page.goto("/admin");
  await expectResolvedUiPreferences(page, "light", "zh");
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2aa"])
    .analyze();
  expect(results.violations.filter((v) => v.id === "color-contrast")).toEqual(
    [],
  );
});

test("trend range exposes its selected state", async ({ page }) => {
  await page.goto("/admin");
  const group = page.getByRole("group", { name: /统计周期|Period/ });
  const selectedRange = group.getByRole("button", {
    name: /24 小时|24h|24 hours/i,
  });
  await selectedRange.click();
  await expect(selectedRange).toHaveAttribute("aria-pressed", "true");
  await expect(
    group.getByRole("button", { name: /1 小时|1h|1 hour/i }),
  ).toHaveAttribute("aria-pressed", "false");
});

test("trend metric selector exposes button and pressed semantics", async ({
  page,
}) => {
  await setUiPreferences(page, "light", "en");
  await page.goto("/admin");
  const group = page.getByRole("group", { name: "Trend metric" });
  const metrics = ["Requests", "Traffic", "Latency", "Errors"].map((name) =>
    group.getByRole("button", { name, exact: true }),
  );
  await expect(group).toBeVisible();
  for (const metric of metrics)
    await expect(metric).toHaveAttribute("type", "button");

  await expect(metrics[0]).toHaveAttribute("aria-pressed", "true");
  await expect(metrics[2]).toHaveAttribute("aria-pressed", "false");
  await metrics[2].click();
  await expect(metrics[0]).toHaveAttribute("aria-pressed", "false");
  await expect(metrics[2]).toHaveAttribute("aria-pressed", "true");
});

test("trend chart exposes the selected metric and range to assistive technology", async ({
  page,
}) => {
  await setUiPreferences(page, "light", "en");
  await mockAdminApi(page, {
    "GET /api/v1/admin/dashboard/trends": { points: populatedTrendPoints },
  });
  await page.goto("/admin");

  const chartDescription = page.locator(
    '[data-query-key="dashboard-trends"] .recharts-wrapper > svg.recharts-surface > desc',
  );
  await expect(chartDescription).toHaveText(
    "Requests trend for 30d. Use the left and right arrow keys to inspect time points.",
    { timeout: CHART_TIMEOUT },
  );

  await page
    .getByRole("group", { name: "Trend metric" })
    .getByRole("button", { name: "Latency" })
    .click();
  await page
    .getByRole("group", { name: /Period|统计周期/ })
    .getByRole("button", { name: "24h" })
    .click();
  await expect(chartDescription).toHaveText(
    "Latency trend for 24h. Use the left and right arrow keys to inspect time points.",
  );
});

test("trend series use linear paths", async ({ page }) => {
  await mockAdminApi(page, {
    "GET /api/v1/admin/dashboard/trends": { points: populatedTrendPoints },
  });
  await page.goto("/admin");

  await expect(
    page.locator('[data-query-key="dashboard-trends"] .recharts-bar-rectangle'),
  ).toHaveCount(1);
});

async function exactTrendLabel(
  page: import("@playwright/test").Page,
  bucket: number,
  range: "1h" | "30d",
) {
  return page.evaluate(
    ({ value, showSeconds }) => {
      const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
      return new Date(value * 1000).toLocaleString(undefined, {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: showSeconds ? "2-digit" : undefined,
        timeZoneName: "short",
        timeZone,
      });
    },
    { value: bucket, showSeconds: range === "1h" },
  );
}

async function hoverTrendEndpoint(
  chart: import("@playwright/test").Locator,
  endpoint: "first" | "last",
) {
  const box = await chart.boundingBox();
  expect(box).not.toBeNull();
  await chart.hover({
    position: { x: endpoint === "first" ? 50 : box!.width - 50, y: 80 },
  });
  return chart.locator(".recharts-tooltip-wrapper p").first();
}

test("1h trend tooltips distinguish adjacent ten-second buckets in local time", async ({
  page,
}) => {
  const points = populatedTrendPoints.slice(0, 2).map((point, index) => ({
    ...point,
    bucket: populatedTrendPoints[0].bucket + index * 10,
  }));
  await mockAdminApi(page, {
    "GET /api/v1/admin/dashboard/trends": { points },
  });
  await page.goto("/admin?range=1h");

  const chart = await trendChart(page);
  const firstExpected = await exactTrendLabel(page, points[0].bucket, "1h");
  const lastExpected = await exactTrendLabel(page, points[1].bucket, "1h");

  expect(firstExpected).not.toBe(lastExpected);
  await expect(await hoverTrendEndpoint(chart, "first")).toContainText(
    firstExpected.slice(0, -3),
  );
  await expect(await hoverTrendEndpoint(chart, "last")).toContainText(
    lastExpected.slice(0, -3),
  );
  await expect(
    chart.locator(".recharts-tooltip-wrapper p").first(),
  ).toContainText("–");
});

test("30d trend tooltips distinguish adjacent twelve-hour display buckets in local time", async ({
  page,
}) => {
  const points = populatedTrendPoints.slice(0, 2).map((point, index) => ({
    ...point,
    bucket: populatedTrendPoints[0].bucket + index * 12 * 60 * 60,
  }));
  await mockAdminApi(page, {
    "GET /api/v1/admin/dashboard/trends": { points },
  });
  await page.goto("/admin");
  await page
    .getByRole("group", { name: /统计周期|Period/ })
    .getByRole("button", { name: /30 天|30d/i })
    .click();

  const chart = await trendChart(page);
  const firstExpected = await exactTrendLabel(page, points[0].bucket, "30d");
  const lastExpected = await exactTrendLabel(page, points[1].bucket, "30d");

  expect(firstExpected).not.toBe(lastExpected);
  await expect(await hoverTrendEndpoint(chart, "first")).toContainText(
    firstExpected.slice(0, -2),
  );
  await expect(await hoverTrendEndpoint(chart, "last")).toContainText(
    lastExpected.slice(0, -2),
  );
  await expect(
    chart.locator(".recharts-tooltip-wrapper p").first(),
  ).toContainText("–");
});

test("primary command resolves from the primary token and changes on hover without a filter", async ({
  page,
}) => {
  await setUiPreferences(page, "light", "zh");
  await page.goto("/admin/upstreams");
  const button = page.getByRole("button", { name: /添加上游源/i });
  // The semantic contract is: rest is exactly the primary token, hover is a
  // distinct press state, and neither is faked with a filter.
  const primary = await resolvedTokenColor(page, "--primary");
  const restingBackground = await button.evaluate(
    (el) => getComputedStyle(el).backgroundColor,
  );
  expect(restingBackground).toBe(primary);
  await button.hover();
  await expect
    .poll(() => button.evaluate((el) => getComputedStyle(el).backgroundColor))
    .not.toBe(restingBackground);
  const hoveredBackground = await button.evaluate(
    (el) => getComputedStyle(el).backgroundColor,
  );
  expect(hoveredBackground).not.toBe(restingBackground);
  expect(await button.evaluate((el) => getComputedStyle(el).filter)).toBe(
    "none",
  );
});

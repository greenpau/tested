// Copyright 2026 Paul Greenberg greenpau@outlook.com
// Licensed under the Apache License, Version 2.0.

import { test, expect } from "@playwright/test";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const root = fileURLToPath(new URL("../../", import.meta.url));
const binary = join(root, "bin/tested");
let temporary, reportURL, incompleteURL, emptyURL;

function run(args, expected) {
  const child = spawnSync(binary, args, {
    cwd: root, encoding: "utf8", timeout: 90_000, maxBuffer: 16 * 1024 * 1024,
  });
  expect(child.error).toBeUndefined();
  expect(child.status, child.stdout + child.stderr).toBe(expected);
}

function importedReport(name, events) {
  const evidence = join(temporary, name + ".jsonl");
  writeFileSync(evidence, events.map(event => JSON.stringify(event)).join("\n") + "\n", { mode: 0o600 });
  const output = join(temporary, name);
  run(["report", "--events", evidence, "-o", output, "--no-coverage", "--quiet"], 2);
  return pathToFileURL(join(output, "test_output.html")).href;
}

function occurrence(page, label) {
  return page.getByRole("heading", { level: 4, name: label, exact: true })
    .locator("xpath=ancestor::article[1]");
}

async function switchView(page, view) {
  await page.getByRole("radio", { name: view, exact: true }).check();
  await expect(page.locator("#report-content")).toHaveAttribute("data-view", view.toLowerCase());
}

test.beforeAll(() => {
  temporary = realpathSync(mkdtempSync(join(tmpdir(), "tested-browser-")));
  const output = join(temporary, "report");
  run(["run", "-C", resolve(root, "testdata/browser"), "-o", output,
    "--no-coverage", "--quiet", "--redact", "secret-(one|two)", "--", "-count=2", "./..."], 1);
  const report = join(output, "test_output.html");
  const first = readFileSync(report);
  run(["report", "-C", resolve(root, "testdata/browser"), "-o", output,
    "--no-coverage", "--quiet", "--allow-failures", "--redact", "secret-(one|two)"], 0);
  expect(readFileSync(report).equals(first), "offline HTML rerender is deterministic").toBe(true);
  expect(first.toString()).not.toMatch(/secret-one|secret-two/);
  reportURL = pathToFileURL(report).href;
  incompleteURL = importedReport("incomplete", [
    { Action: "start", Package: "partial" },
    { Action: "run", Package: "partial", Test: "TestPartial" },
    { Action: "run", Package: "partial", Test: "TestPartial/child" },
    { Action: "output", Package: "partial", Test: "TestPartial/child", Output: "retained incomplete evidence\n" },
    { Action: "start", Package: "orphan" },
    { Action: "run", Package: "orphan", Test: "TestMissing/leaf" },
    { Action: "pass", Package: "orphan", Test: "TestMissing/leaf", Elapsed: 0 },
    { Action: "pass", Package: "orphan", Elapsed: 0 },
    { Action: "build-output", ImportPath: "broken/package", Output: "compiler diagnostic\n" },
    { Action: "build-fail", ImportPath: "broken/package" },
  ]);
  emptyURL = importedReport("empty", []);
});

test.afterAll(() => {
  if (temporary) rmSync(temporary, { recursive: true, force: true });
});

test.beforeEach(async ({ page }) => {
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => { if (message.type() === "error") errors.push(message.text()); });
  page.on("request", request => {
    if (!request.url().startsWith("file:")) errors.push("Remote request: " + request.url());
  });
  page.errors = errors;
  await page.goto(reportURL);
});

test.afterEach(async ({ page }) => {
  expect(page.errors, "no runtime errors, CSP violations, or remote requests").toEqual([]);
});

test("flat and nested views preserve every occurrence, exact parent, and open output", async ({ page }) => {
  await expect(page.getByRole("radio", { name: "Flat", exact: true })).toBeChecked();
  const ids = await page.locator(".test-list > .test-row").evaluateAll(rows => rows.map(row => row.id));
  const passing = occurrence(page, "TestTree/group/pass");
  await passing.locator("summary").click();
  await passing.evaluate(row => { row.retainedIdentity = true; });
  await switchView(page, "Nested");
  await expect(page.locator(".test-list > .test-row")).toHaveCount(8);
  expect(await page.locator(".test-row").evaluateAll(rows => rows.every(row => {
    const parent = row.parentElement.closest(".test-row");
    return (parent?.id || "") === row.dataset.parent;
  }))).toBe(true);
  const uneven = occurrence(page, "TestUneven/only-second-run");
  await expect(uneven.locator("xpath=../..").locator(":scope > .test-content h4"))
    .toHaveText("TestUneven [attempt 2]");
  // Four visually identical redacted leaf names retain four distinct parents.
  const redactedParents = await page.locator('.test-row h4').evaluateAll(headings =>
    headings.filter(h => h.textContent.includes("<img_src=")).map(h => h.closest(".test-row").dataset.parent));
  expect(new Set(redactedParents).size).toBe(4);
  for (let i = 0; i < 3; i++) {
    await switchView(page, "Flat");
    expect(await page.locator(".test-list > .test-row").evaluateAll(rows => rows.map(row => row.id))).toEqual(ids);
    await expect(passing.locator("details")).toHaveAttribute("open", "");
    expect(await passing.evaluate(row => row.retainedIdentity)).toBe(true);
    await switchView(page, "Nested");
  }
  await expect(page.locator(".test-row")).toHaveCount(ids.length);
  expect(await page.evaluate(() => window.injected)).toBeUndefined();
  await expect(page.locator("img")).toHaveCount(0);
});

test("filters reveal ancestors and temporarily expand collapsed branches in both views", async ({ page }) => {
  await switchView(page, "Nested");
  await page.getByRole("button", { name: "Collapse all", exact: true }).click();
  await expect(occurrence(page, "TestTree/group/fail")).toBeHidden();
  await page.getByRole("searchbox").fill("unique-leaf-failure");
  await expect(occurrence(page, "TestTree/group/fail")).toBeVisible();
  await expect(occurrence(page, "TestTree/group")).toBeVisible();
  await expect(occurrence(page, "TestTree")).toBeVisible();
  await expect(occurrence(page, "TestTree/group/pass")).toBeHidden();
  await expect(page.getByRole("button", { name: "Collapse all", exact: true })).toBeDisabled();
  const count = (await page.locator("#match-count").textContent()).split(" · ")[0];
  await switchView(page, "Flat");
  await expect(occurrence(page, "TestTree/group/fail")).toBeVisible();
  await expect(occurrence(page, "TestTree")).toBeHidden();
  await expect(page.locator("#match-count")).toHaveText(count + " · Flat view");
  await switchView(page, "Nested");
  await page.getByRole("searchbox").fill("");
  await expect(occurrence(page, "TestTree/group/fail")).toBeHidden();
  await page.getByLabel("Status", { exact: true }).selectOption("skipped");
  await expect(occurrence(page, "TestTree/group/skip")).toBeVisible();
  await expect(occurrence(page, "TestTree/group")).toBeVisible();
  await expect(occurrence(page, "TestTree/group/fail")).toBeHidden();
  await page.getByRole("searchbox").fill("no-such-result-98347");
  await expect(page.locator(".filterable:visible")).toHaveCount(0);
  await expect(page.locator("#match-count")).toHaveText("0 matching results · Nested view");
  await page.getByRole("searchbox").fill("");
  await page.getByLabel("Status", { exact: true }).selectOption("");
  await page.getByRole("button", { name: "Expand all", exact: true }).click();
  await expect(occurrence(page, "TestTree/group/fail")).toBeVisible();
  await page.getByRole("searchbox").fill("example.com/tested-browser");
  const total = await page.locator(".test-row").count();
  await expect(page.locator(".test-row:visible")).toHaveCount(total);
});

test("radio and branch controls work from the keyboard with visible focus", async ({ page }) => {
  const flat = page.getByRole("radio", { name: "Flat", exact: true });
  await flat.focus();
  await page.keyboard.press("ArrowRight");
  await expect(page.getByRole("radio", { name: "Nested", exact: true })).toBeChecked();
  const button = occurrence(page, "TestTree").locator(":scope > .test-content > .branch-toggle");
  await button.focus();
  expect(await button.evaluate(element => getComputedStyle(element).outlineStyle)).not.toBe("none");
  await page.keyboard.press("Enter");
  await expect(button).toHaveAttribute("aria-expanded", "false");
  await expect(occurrence(page, "TestTree/group")).toBeHidden();
  await page.keyboard.press("Space");
  await expect(button).toHaveAttribute("aria-expanded", "true");
  await expect(occurrence(page, "TestTree/group")).toBeVisible();
  const controlled = await button.getAttribute("aria-controls");
  await expect(page.locator(`[id="${controlled}"]`)).toBeVisible();
});

test("incomplete, orphan, build-only, and empty evidence remain inspectable", async ({ page }) => {
  await page.goto(incompleteURL);
  await switchView(page, "Nested");
  await expect(occurrence(page, "TestMissing/leaf")).toBeVisible();
  await expect(occurrence(page, "TestMissing/leaf")).toHaveAttribute("data-parent", "");
  await page.getByLabel("Status", { exact: true }).selectOption("incomplete");
  await expect(occurrence(page, "TestPartial/child")).toBeVisible();
  await expect(occurrence(page, "TestPartial/child").locator("details")).toHaveAttribute("open", "");
  await page.getByLabel("Status", { exact: true }).selectOption("failed");
  await expect(page.getByRole("heading", { name: "broken/package", exact: true })).toBeVisible();
  await page.goto(emptyURL);
  await switchView(page, "Nested");
  await expect(page.getByText("No package events were decoded.")).toBeVisible();
  await expect(page.locator("#tree-controls")).toBeHidden();
});

test("printing reveals filtered and collapsed evidence and restores screen state", async ({ page, browserName }, testInfo) => {
  await switchView(page, "Nested");
  await page.getByRole("button", { name: "Collapse all", exact: true }).click();
  await page.getByLabel("Status", { exact: true }).selectOption("skipped");
  const total = await page.locator(".test-row").count();
  const closedBefore = await page.locator("details:not([open])").count();
  await page.emulateMedia({ media: "print" });
  await page.evaluate(() => window.dispatchEvent(new Event("beforeprint")));
  await expect(page.locator(".test-row:visible")).toHaveCount(total);
  await expect(page.locator("details:not([open])")).toHaveCount(0);
  await expect(page.locator(".branch-toggle:visible")).toHaveCount(0);
  await expect(page.locator("#report-controls")).toBeHidden();
  await expect(occurrence(page, "TestTree/group/pass").locator("pre")).toBeVisible();
  await page.evaluate(() => window.dispatchEvent(new Event("afterprint")));
  await expect(page.locator("details:not([open])")).toHaveCount(closedBefore);
  if (browserName === "chromium") {
    await page.pdf({ path: testInfo.outputPath("nested-report.pdf"), format: "A4" });
    await expect(page.locator("details:not([open])")).toHaveCount(closedBefore);
  }
  await page.emulateMedia({ media: "screen" });
  await page.getByLabel("Status", { exact: true }).selectOption("");
  await expect(occurrence(page, "TestTree/group/pass")).toBeHidden();
  await switchView(page, "Flat");
  await page.emulateMedia({ media: "print" });
  await expect(page.locator(".test-row:visible")).toHaveCount(total);
  await expect(page.locator(".branch-toggle:visible")).toHaveCount(0);
  await expect(page.locator("#report-controls")).toBeHidden();
});

for (const colorScheme of ["light", "dark"]) {
  test(`nested layout fits desktop and mobile in ${colorScheme} mode`, async ({ page }, testInfo) => {
    await page.emulateMedia({ colorScheme });
    await switchView(page, "Nested");
    for (const viewport of [{ width: 1440, height: 1000 }, { width: 390, height: 844 }]) {
      await page.setViewportSize(viewport);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      for (const name of ["Flat", "Nested"]) {
        const box = await page.getByRole("radio", { name, exact: true }).boundingBox();
        expect(box.x).toBeGreaterThanOrEqual(0);
        expect(box.x + box.width).toBeLessThanOrEqual(viewport.width);
      }
      await occurrence(page, "TestTree").evaluate(row => {
        const toolbar = document.getElementById("report-controls");
        window.scrollTo(0, row.getBoundingClientRect().top + scrollY - toolbar.offsetHeight - 12);
      });
      await page.screenshot({ path: testInfo.outputPath(`nested-${colorScheme}-${viewport.width}.png`) });
    }
  });
}

test("JavaScript-disabled reports retain all flat evidence without inactive controls", async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false });
  try {
    const page = await context.newPage();
    await page.goto(reportURL);
    await expect(page.locator("#report-controls")).toBeHidden();
    const total = await page.locator(".test-row").count();
    await expect(page.locator(".test-list > .test-row:visible")).toHaveCount(total);
    await expect(occurrence(page, "TestTree/group/pass").locator("pre")).toBeVisible();
    await occurrence(page, "TestTree/group/pass").locator("summary").click();
    await expect(occurrence(page, "TestTree/group/pass").locator("pre")).toBeHidden();
    await occurrence(page, "TestTree/group/pass").locator("summary").click();
    await page.emulateMedia({ media: "print" });
    await expect(page.locator("#report-controls")).toBeHidden();
    await expect(occurrence(page, "TestTree/group/skip").locator("pre")).toBeVisible();
  } finally {
    await context.close();
  }
});

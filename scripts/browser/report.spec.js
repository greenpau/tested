// Copyright 2026 Paul Greenberg greenpau@outlook.com
// Licensed under the Apache License, Version 2.0.

import { test, expect } from "@playwright/test";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const root = fileURLToPath(new URL("../../", import.meta.url));
const binary = join(root, "bin/tested");
let temporary, reportURL, incompleteURL, emptyURL, filterURL, sortingURL, limitedURL, packagesURL, relativeURL, changesURL, unchangedURL, coverageFallbackURL, moduleBlockURL, redactedCoverageURL;

function run(args, expected) {
  const child = spawnSync(binary, args, {
    cwd: root, encoding: "utf8", timeout: 90_000, maxBuffer: 16 * 1024 * 1024,
  });
  expect(child.error).toBeUndefined();
  expect(child.status, child.stdout + child.stderr).toBe(expected);
}

function importedReport(name, events, { directory = root, coverprofile, slowest, redact = [] } = {}) {
  const evidence = join(temporary, name + ".jsonl");
  writeFileSync(evidence, events.map(event => JSON.stringify(event)).join("\n") + "\n", { mode: 0o600 });
  const output = join(temporary, name);
  run(["report", "-C", directory, "--events", evidence, "-o", output,
    ...(coverprofile ? ["--coverprofile", coverprofile] : ["--no-coverage"]),
    ...(slowest === undefined ? [] : ["--slowest", String(slowest)]),
    ...redact.flatMap(pattern => ['--redact', pattern]), "--quiet"], 2);
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
    { Action: "output", Package: "partial", Output: "incomplete package evidence\n" },
    { Action: "attr", Package: "partial", Key: "owner", Value: "partial-owner" },
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
  const moduleDirectory = join(temporary, 'module-block');
  mkdirSync(moduleDirectory, { mode: 0o700 });
  writeFileSync(join(moduleDirectory, 'go.mod'),
    'require (\n module v1.0.0\n)\nmodule example.com/block\n\ngo 1.25.0\n', { mode: 0o600 });
  moduleBlockURL = importedReport('module-block-report', [
    { Action: 'start', Package: 'example.com/block' },
    { Action: 'pass', Package: 'example.com/block', Elapsed: 0 },
    { Action: 'start', Package: 'example.com/block/child' },
    { Action: 'pass', Package: 'example.com/block/child', Elapsed: 0 },
  ], { directory: moduleDirectory });
  const filterEvents = [];
  for (const suffix of ["alpha", "beta"]) {
    const Package = "example.com/" + suffix;
    const Action = suffix === "alpha" ? "pass" : "fail";
    filterEvents.push(
      { Action: "start", Package },
      { Action: "output", Package, Output: "package-only transcript\n" },
      { Action: "run", Package, Test: "TestRoot" },
      { Action: "output", Package, Test: "TestRoot", Output: "parent-only transcript\n" },
      { Action: "run", Package, Test: "TestRoot/Leaf[1]" },
      { Action: "output", Package, Test: "TestRoot/Leaf[1]", Output: "shared-output <img src=x onerror=alert(1)> literal.*\n" },
      { Action, Package, Test: "TestRoot/Leaf[1]", Elapsed: 0 },
      { Action, Package, Test: "TestRoot", Elapsed: 0 },
      { Action: "run", Package, Test: "TestNameOnly" },
      { Action: "attr", Package, Test: "TestNameOnly", Key: "owner", Value: "metadata-only-token" },
      { Action: "output", Package, Test: "TestNameOnly", Output: "example.com/decoy TestDecoy\n" },
      { Action: "pass", Package, Test: "TestNameOnly", Elapsed: 0 },
      { Action, Package, Elapsed: 0 },
    );
  }
  filterEvents.push(
    { Action: "build-output", ImportPath: "compiler/package", Output: "compiler-only-token\n" },
    { Action: "build-fail", ImportPath: "compiler/package" },
    { Action: "notice", Output: "unattributed-only-token\n" },
    { Action: 17, Broken: "diagnostic-only-token" },
  );
  filterURL = importedReport("filters", filterEvents);
  const directory = join(temporary, "sortmodule");
  mkdirSync(directory, { mode: 0o700 });
  writeFileSync(join(directory, "go.mod"), "module example.com/sort\n\ngo 1.25.0\n", { mode: 0o600 });
  const profile = ["mode: set"];
  for (const [index, [pkg, file, covered, total]] of [
    ["pkg2", "file10.go", 2n, 10n], ["pkg2", "file2.go", 10n, 100n],
    ["pkg10", "file1.go", 9n, 10n], ["pkg1", "file3.go", 9007199254740992n, 9007199254740993n],
    ["pkg1", "file4.go", 9007199254740993n, 9007199254740994n], ["pkg3", "no.go", 0n, 0n],
  ].entries()) {
    mkdirSync(join(directory, pkg), { recursive: true, mode: 0o700 });
    writeFileSync(join(directory, pkg, file), `package ${pkg}\nfunc Value${index}() int {\n return 1\n}\n`, { mode: 0o600 });
    profile.push(`example.com/sort/${pkg}/${file}:2.1,2.20 ${covered} 1`);
    profile.push(`example.com/sort/${pkg}/${file}:3.1,3.10 ${total - covered} 0`);
  }
  const coverprofile = join(directory, "input.cover");
  writeFileSync(coverprofile, profile.join("\n") + "\n", { mode: 0o600 });
  const sortingEvents = [];
  for (const [pkg, Test, Elapsed, Action] of [
    ["pkg2", "Test10", 0.9, "pass"], ["pkg2", "Test2", 2, "fail"],
    ["pkg10", "Test1", 0.0000012, "skip"], ["pkg1", "Test3", 10, "pass"],
    ["pkg1", "Test4", 2, "pass"],
  ]) {
    const Package = "example.com/sort/" + pkg;
    sortingEvents.push({ Action: "run", Package, Test }, { Action, Package, Test, Elapsed });
  }
  sortingURL = importedReport("sorting", sortingEvents, { directory, coverprofile });
  redactedCoverageURL = importedReport('redacted-coverage', sortingEvents, {
    directory, coverprofile, redact: ['^example[.]com/sort/pkg2/file10[.]go$'],
  });
  writeFileSync(join(directory, "root.go"), "package sort\nfunc Root() int { return 1 }\n", { mode: 0o600 });
  const relativeProfile = join(directory, "relative.cover");
  writeFileSync(relativeProfile, [...profile, "example.com/sort/root.go:2.1,2.29 1 1"].join("\n") + "\n", { mode: 0o600 });
  const relativeEvents = [...sortingEvents];
  for (const Package of ['example.com/sort', 'example.com/sort-extra/pkg', 'other.example/pkg']) {
    for (const Test of ['TestRoot1', 'TestRoot2']) {
      relativeEvents.push({ Action: 'run', Package, Test }, { Action: 'pass', Package, Test, Elapsed: 1 });
    }
    relativeEvents.push({ Action: 'pass', Package, Elapsed: 2 });
  }
  relativeURL = importedReport('relative', relativeEvents, {
    directory: join(directory, 'pkg2'), coverprofile: relativeProfile, slowest: 20,
  });
  const packageEvents = [];
  for (const [suffix, Action, Elapsed] of [
    ['pkg1', 'pass', 10], ['pkg2', 'fail', 0.9], ['pkg10', 'skip', 2], ['pkg3', 'pass', 0],
  ]) {
    const Package = 'example.com/sort/' + suffix;
    packageEvents.push({ Action: 'start', Package });
    if (suffix !== 'pkg3') {
      packageEvents.push(
        { Action: 'output', Package, Output: 'package transcript\n' },
        { Action: 'attr', Package, Key: 'owner', Value: 'package-owner' },
        { Action: 'run', Package, Test: 'TestPackage' },
        { Action: 'run', Package, Test: 'TestPackage/child' },
        { Action: 'output', Package, Test: 'TestPackage/child', Output: 'child transcript\n' },
        { Action, Package, Test: 'TestPackage/child', Elapsed },
        { Action, Package, Test: 'TestPackage', Elapsed },
      );
    }
    packageEvents.push({ Action, Package, Elapsed });
  }
  packageEvents.push({ Action: 'start', Package: 'example.com/sort/pkg4' });
  packagesURL = importedReport('packages', packageEvents, { directory, coverprofile });
  const changedDirectory = join(temporary, 'changes-module');
  mkdirSync(changedDirectory, { mode: 0o700 });
  const git = (...args) => {
    const child = spawnSync('git', ['-c', 'user.name=Tested Browser', '-c', 'user.email=tested@example.invalid',
      '-c', 'commit.gpgsign=false', '-c', 'core.hooksPath=' + join(changedDirectory, '.hooks'), ...args],
      { cwd: changedDirectory, encoding: 'utf8', timeout: 30_000 });
    expect(child.error).toBeUndefined();
    expect(child.status, child.stdout + child.stderr).toBe(0);
  };
  const writePackage = (pkg, value = 1, file = 'value.go') => {
    const target = join(changedDirectory, pkg);
    mkdirSync(target, { recursive: true, mode: 0o700 });
    writeFileSync(join(target, file), `package sample\nfunc Value() int { return ${value} }\n`, { mode: 0o600 });
    writeFileSync(join(target, 'value_test.go'), `package sample
import "testing"
func TestValue(t *testing.T) {
 t.Run("child", func(t *testing.T) { if Value() < 1 { t.Fatal("value") }; t.Log("retained child output") })
}
`, { mode: 0o600 });
  };
  writeFileSync(join(changedDirectory, 'go.mod'), 'module example.com/changes\n\ngo 1.25.0\n', { mode: 0o600 });
  for (const pkg of ['.', 'changed', 'stable', 'testonly']) writePackage(pkg);
  writePackage('renamed', 1, 'old.go');
  git('init', '-q');
  git('add', '--', '.');
  git('commit', '-q', '-m', 'fixture baseline');
  const cleanOutput = join(temporary, 'unchanged-report');
  const changedOutput = join(temporary, 'changed-report');
  run(['run', '-C', changedDirectory, '-o', cleanOutput, '--coverage-diff-base', 'HEAD', '--quiet', '--', '-count=1', './...'], 0);
  unchangedURL = pathToFileURL(join(cleanOutput, 'test_output.html')).href;
  writePackage('changed', 2);
  git('mv', 'renamed/old.go', 'renamed/new.go');
  writePackage('fresh');
  writeFileSync(join(changedDirectory, 'testonly/value_test.go'), readFileSync(join(changedDirectory, 'testonly/value_test.go'), 'utf8') + '// test-only edit\n', { mode: 0o600 });
  run(['run', '-C', changedDirectory, '-o', changedOutput, '--coverage-diff-base', 'HEAD', '--quiet', '--', '-count=2', './...'], 0);
  changesURL = pathToFileURL(join(changedOutput, 'test_output.html')).href;
  const original = readFileSync(join(changedOutput, 'test_output.html'));
  run(['report', '-C', changedDirectory, '-o', changedOutput, '--coverage-diff-base', 'HEAD', '--quiet'], 0);
  expect(readFileSync(join(changedOutput, 'test_output.html')).equals(original), 'change membership survives offline rerender').toBe(true);
  const failedOutput = join(temporary, 'failed-comparison');
  run(['report', '-C', changedDirectory, '-o', failedOutput,
    '--events', join(changedOutput, 'test_output.jsonl'),
    '--coverprofile', join(changedOutput, 'coverage.out'),
    '--coverage-diff-base', 'refs/heads/absent-fixture-baseline', '--quiet'], 2);
  expect(readFileSync(join(failedOutput, 'test_output.html'), 'utf8')).not.toContain('<input id="changed-packages-only"');
  const fallback = join(changedOutput, 'coverage-fallback.html');
  writeFileSync(fallback, readFileSync(join(changedOutput, 'coverage.html'), 'utf8').replace('id="nav"', 'id="unsupported-nav"'), { mode: 0o600 });
  coverageFallbackURL = pathToFileURL(fallback).href;
  limitedURL = importedReport("limited", [
    ...sortingEvents,
    { Action: "run", Package: "example.com/sort/pkg2", Test: "Test10" },
    { Action: "pass", Package: "example.com/sort/pkg2", Test: "Test10", Elapsed: 12 },
    { Action: "run", Package: "example.com/sort/pkg1", Test: "Test3" },
    { Action: "pass", Package: "example.com/sort/pkg1", Test: "Test3", Elapsed: 8 },
  ], { slowest: 3 });
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
  await page.getByRole("searchbox", { name: "Output", exact: true }).fill("unique-leaf-failure");
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
  await page.getByRole("searchbox", { name: "Output", exact: true }).fill("");
  await expect(occurrence(page, "TestTree/group/fail")).toBeHidden();
  await page.getByLabel("Status", { exact: true }).selectOption("skipped");
  await expect(occurrence(page, "TestTree/group/skip")).toBeVisible();
  await expect(occurrence(page, "TestTree/group")).toBeVisible();
  await expect(occurrence(page, "TestTree/group/fail")).toBeHidden();
  await page.getByRole("searchbox", { name: "Output", exact: true }).fill("no-such-result-98347");
  await expect(page.locator(".filterable:visible")).toHaveCount(0);
  await expect(page.locator("#match-count")).toHaveText("0 matching results · Nested view");
  await page.getByRole("searchbox", { name: "Output", exact: true }).fill("");
  await page.getByLabel("Status", { exact: true }).selectOption("");
  await page.getByRole("button", { name: "Expand all", exact: true }).click();
  await expect(occurrence(page, "TestTree/group/fail")).toBeVisible();
  await page.getByRole("searchbox", { name: "Package", exact: true }).fill("example.com/tested-browser");
  const total = await page.locator(".test-row").count();
  await expect(page.locator(".test-row:visible")).toHaveCount(total);
});

test("radio and branch controls work from the keyboard with visible focus", async ({ page }) => {
  const flat = page.getByRole("radio", { name: "Flat", exact: true });
  await flat.focus();
  await page.keyboard.press("ArrowRight");
  await expect(page.getByRole("radio", { name: "Nested", exact: true })).toBeChecked();
  const button = occurrence(page, "TestTree").locator(":scope > .test-content > .test-heading > .branch-toggle");
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

test("compact test rows keep package context and expandable evidence", async ({ page }, testInfo) => {
  await page.goto(filterURL);
  const pkg = page.locator('.package-row[data-package="example.com/alpha"]');
  const row = pkg.locator('[data-test="TestNameOnly"]');
  const details = row.locator('.test-details');
  for (const view of ["Flat", "Nested"]) {
    await switchView(page, view);
    await expect(pkg.locator('.package-header h3')).toHaveText("example.com/alpha");
    for (const heading of await pkg.locator('.test-heading').all()) {
      await expect(heading).not.toContainText("example.com/");
      await expect(heading).not.toContainText("occurrence");
      await expect(heading).not.toContainText("go_elapsed");
    }
    // Measure the row's own content, excluding descendants in Nested view.
    for (const content of await pkg.locator('.test-content').all()) {
      const box = await content.boundingBox();
      expect(box.height).toBeLessThanOrEqual(40);
    }
    await expect(row.locator('.test-duration')).toHaveText("0s");
    await expect(details.locator('.meta')).toBeHidden();
    await details.locator('summary').focus();
    await page.keyboard.press('Enter');
    await expect(details.locator('.meta')).toContainText("occurrence 1 · test · 0s (go_elapsed)");
    await expect(details.locator('.meta')).toBeVisible();
    await expect(details.locator('pre')).toHaveText("example.com/decoy TestDecoy\n");
    await expect(details.getByRole('cell', { name: 'metadata-only-token', exact: true })).toBeVisible();
    await page.keyboard.press('Enter');
    await expect(details.locator('pre')).toBeHidden();
    await pkg.screenshot({ path: testInfo.outputPath(`compact-${view.toLowerCase()}.png`) });
  }
  const failed = page.locator('.package-row[data-package="example.com/beta"] [data-test="TestRoot/Leaf[1]"]');
  await expect(failed.locator('pre')).toBeVisible();
});

test("tables default to natural package order and sort strings, durations, and exact coverage values", async ({ page }, testInfo) => {
  await page.goto(sortingURL);
  const slowest = page.getByRole('table', { name: 'Slowest occurrences', exact: true });
  const coverage = page.getByRole('table', { name: 'Weighted coverage by file', exact: true });
  const column = async (table, index) => table.locator('tbody tr').evaluateAll(
    (rows, index) => rows.map(row => row.cells[index].textContent.trim()), index);
  const sortBy = async (table, name) => table.getByRole('button', { name: 'Sort by ' + name, exact: true }).click();
  const assertPackageContext = async table => {
    await expect(table.locator('tbody [data-package-label].sr-only')).toHaveCount(0);
    for (const cell of await table.locator('tbody tr td:first-child').all()) {
      await expect(cell).not.toHaveText('');
      await expect(cell.locator('[data-package-label]')).toBeVisible();
      expect(await cell.innerText()).toBe(await cell.textContent());
    }
  };
  expect(await column(slowest, 0)).toEqual([
    'pkg1', 'pkg1', 'pkg2',
    'pkg2', 'pkg10',
  ]);
  expect(await column(coverage, 0)).toEqual([
    'pkg1', 'pkg1', 'pkg2',
    'pkg2', 'pkg3', 'pkg10',
  ]);
  await expect(slowest.locator('th[aria-sort="ascending"]')).toHaveText('Package');
  await expect(coverage.locator('th[aria-sort="ascending"]')).toHaveText('Package');
  await slowest.locator('tbody tr').first().evaluate(row => { row.retainedIdentity = true; });
  await sortBy(slowest, 'Occurrence');
  expect(await column(slowest, 1)).toEqual(['Test1', 'Test2', 'Test3', 'Test4', 'Test10']);
  await sortBy(slowest, 'Occurrence');
  expect(await column(slowest, 1)).toEqual(['Test10', 'Test4', 'Test3', 'Test2', 'Test1']);
  await sortBy(slowest, 'Duration');
  expect(await column(slowest, 1)).toEqual(['Test1', 'Test10', 'Test4', 'Test2', 'Test3']);
  await sortBy(slowest, 'Duration');
  expect(await column(slowest, 1)).toEqual(['Test3', 'Test4', 'Test2', 'Test10', 'Test1']);
  await assertPackageContext(slowest);
  await sortBy(slowest, 'Status');
  expect((await column(slowest, 4)).map(value => value.toLowerCase())).toEqual(['failed', 'passed', 'passed', 'passed', 'skipped']);
  await sortBy(slowest, 'Source');
  // Equal values retain their original order, independent of previous sorts.
  expect(await column(slowest, 1)).toEqual(['Test3', 'Test4', 'Test2', 'Test10', 'Test1']);
  expect(await slowest.locator('tbody tr').evaluateAll(rows => rows.filter(row => row.retainedIdentity).length)).toBe(1);

  await sortBy(coverage, 'File');
  expect(await column(coverage, 1)).toEqual(['file1.go', 'file2.go', 'file3.go', 'file4.go', 'file10.go', 'no.go']);
  await sortBy(coverage, 'Covered statements');
  expect(await column(coverage, 2)).toEqual(['0', '2', '9', '10', '9007199254740992', '9007199254740993']);
  await sortBy(coverage, 'Covered statements');
  expect(await column(coverage, 2)).toEqual(['9007199254740993', '9007199254740992', '10', '9', '2', '0']);
  await sortBy(coverage, 'Total statements');
  expect(await column(coverage, 3)).toEqual(['0', '10', '10', '100', '9007199254740993', '9007199254740994']);
  await sortBy(coverage, 'Coverage');
  expect(await column(coverage, 1)).toEqual(['file2.go', 'file10.go', 'file1.go', 'file3.go', 'file4.go', 'no.go']);
  await sortBy(coverage, 'Coverage');
  expect(await column(coverage, 1)).toEqual(['file4.go', 'file3.go', 'file1.go', 'file10.go', 'file2.go', 'no.go']);
  await assertPackageContext(coverage);
  await expect(coverage.locator('th[aria-sort="descending"]')).toHaveText('Coverage');
  await expect(coverage.locator('th[aria-sort]')).toHaveCount(1);
  const packageButton = coverage.getByRole('button', { name: 'Sort by Package', exact: true });
  await packageButton.focus();
  await page.keyboard.press('Enter');
  await expect(coverage.locator('th[aria-sort="ascending"]')).toHaveText('Package');
  await page.keyboard.press('Space');
  await expect(coverage.locator('th[aria-sort="descending"]')).toHaveText('Package');
  expect((await column(coverage, 0))[0]).toBe('pkg10');
  await assertPackageContext(coverage);
  await expect(slowest.locator('th[aria-sort="ascending"]')).toHaveText('Source');
  await page.getByRole('button', { name: 'Clear all filters', exact: true }).click();
  await expect(coverage.locator('th[aria-sort="descending"]')).toHaveText('Package');
  await switchView(page, 'Nested');
  await expect(coverage.locator('th[aria-sort="descending"]')).toHaveText('Package');
  await coverage.screenshot({ path: testInfo.outputPath('sorted-coverage.png') });
  await slowest.screenshot({ path: testInfo.outputPath('sorted-slowest.png') });
});

test("slowest sorting retains the requested longest occurrences and repeated attempts", async ({ page }) => {
  await page.goto(limitedURL);
  const slowest = page.getByRole('table', { name: 'Slowest occurrences', exact: true });
  const labels = slowest.locator('tbody tr td:nth-child(2)');
  await expect(labels).toHaveText(['Test3', 'Test3 [attempt 2]', 'Test10 [attempt 2]']);
  const duration = slowest.getByRole('button', { name: 'Sort by Duration', exact: true });
  await duration.click();
  await expect(labels).toHaveText(['Test3 [attempt 2]', 'Test3', 'Test10 [attempt 2]']);
  await duration.click();
  await expect(labels).toHaveText(['Test10 [attempt 2]', 'Test3', 'Test3 [attempt 2]']);
  await slowest.getByRole('button', { name: 'Sort by Package', exact: true }).click();
  await expect(labels).toHaveText(['Test3', 'Test3 [attempt 2]', 'Test10 [attempt 2]']);
});

test("package rows have compact inline metrics and form one continuous list", async ({ page }, testInfo) => {
  await page.goto(packagesURL);
  const rows = page.locator('#package-list > .package-row');
  await expect(rows).toHaveCount(5);
  const header = page.locator('.package-row[data-package="example.com/sort/pkg1"] > .package-header');
  await expect(header.locator('.package-duration')).toHaveText('Duration 10s');
  await expect(header.locator('.package-coverage')).toHaveText('Coverage >99.99%');
  await expect(page.locator('.package-row[data-package="example.com/sort/pkg2"] .package-coverage')).toHaveText('Coverage 10.91%');
  await expect(page.locator('.package-row[data-package="example.com/sort/pkg3"] .package-duration')).toHaveText('Duration 0s');
  await expect(page.locator('.package-row[data-package="example.com/sort/pkg4"] .package-duration')).toHaveText('Duration unavailable');
  await expect(page.locator('.package-row[data-package="example.com/sort/pkg4"] .package-coverage')).toHaveText('Coverage unavailable');
  const name = await header.locator('h3').boundingBox();
  const duration = await header.locator('.package-duration').boundingBox();
  expect(duration.x).toBeGreaterThanOrEqual(name.x + name.width);
  expect(Math.abs(duration.y - name.y)).toBeLessThan(10);
  const coverage = await header.locator('.package-coverage').boundingBox();
  expect(coverage.x - (duration.x + duration.width)).toBeGreaterThanOrEqual(0);
  expect(coverage.x - (duration.x + duration.width)).toBeLessThanOrEqual(16);
  expect(Math.abs(coverage.y - duration.y)).toBeLessThan(2);
  expect(duration.x - (name.x + name.width)).toBeGreaterThan(100);
  expect((await header.boundingBox()).height).toBeLessThanOrEqual(44);
  await page.getByRole('button', { name: 'Collapse all packages', exact: true }).click();
  await expect(page.locator('.package-contents:visible')).toHaveCount(0);
  const boxes = await rows.evaluateAll(elements => elements.map(row => ({
    top: row.getBoundingClientRect().top, bottom: row.getBoundingClientRect().bottom,
    radius: getComputedStyle(row).borderRadius,
  })));
  for (const [index, box] of boxes.entries()) {
    expect(box.radius).toBe('0px');
    if (index) expect(box.top - boxes[index - 1].bottom).toBeLessThanOrEqual(1);
  }
  for (const colorScheme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme });
    for (const width of [1440, 900, 390, 320]) {
      await page.setViewportSize({ width, height: 1000 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await page.locator('#package-list').screenshot({ path: testInfo.outputPath(`packages-${colorScheme}-${width}.png`) });
    }
  }
});

test("package disclosures preserve output, nesting, filtering, and mixed global state", async ({ page }) => {
  await page.goto(packagesURL);
  const pkg = page.locator('.package-row[data-package="example.com/sort/pkg1"]');
  const toggle = pkg.locator('.package-toggle');
  const content = pkg.locator('.package-contents');
  const output = pkg.locator('[data-test="TestPackage/child"] .test-details');
  await output.locator('summary').click();
  await switchView(page, 'Nested');
  const branch = pkg.locator('[data-test="TestPackage"] > .test-content .branch-toggle');
  await branch.click();
  await toggle.focus();
  await page.keyboard.press('Enter');
  await expect(content).toBeHidden();
  await expect(toggle).toHaveAttribute('aria-expanded', 'false');
  await expect(pkg.locator('.package-header')).toBeVisible();
  await expect(output).toHaveAttribute('open', '');
  await page.keyboard.press('Space');
  await expect(content).toBeVisible();
  await expect(branch).toHaveAttribute('aria-expanded', 'false');
  await expect(output).toBeHidden();
  await page.getByRole('button', { name: 'Collapse all packages', exact: true }).click();
  await expect(page.locator('.package-contents:visible')).toHaveCount(0);
  await toggle.click();
  await expect(page.locator('.package-contents:visible')).toHaveCount(1);
  await page.getByRole('button', { name: 'Collapse all packages', exact: true }).click();
  await page.getByRole('searchbox', { name: 'Package', exact: true }).fill('pkg1');
  await page.getByRole('searchbox', { name: 'Test name', exact: true }).fill('TestPackage/child');
  await expect(page.locator('#match-count')).toHaveText('2 matching results · Nested view');
  await expect(page.locator('.package-contents:visible')).toHaveCount(0);
  await page.getByRole('button', { name: 'Expand all packages', exact: true }).click();
  await expect(output).toBeVisible();
  await page.getByRole('button', { name: 'Collapse all packages', exact: true }).click();
  await page.getByRole('button', { name: 'Clear all filters', exact: true }).click();
  await expect(page.locator('.package-contents:visible')).toHaveCount(0);
  await page.getByRole('button', { name: 'Expand all packages', exact: true }).click();
  await expect(page.locator('.package-contents:visible')).toHaveCount(5);
  await expect(branch).toHaveAttribute('aria-expanded', 'false');
  await branch.click();
  await expect(output).toBeVisible();
  await switchView(page, 'Flat');
  await expect(output).toBeVisible();
});

test("failed and incomplete package evidence starts open and survives package toggles", async ({ page }) => {
  for (const [url, name] of [[packagesURL, 'example.com/sort/pkg2'], [incompleteURL, 'partial']]) {
    await page.goto(url);
    const pkg = page.locator('.package-row').filter({ has: page.locator(`h3[title="${name}"]`) });
    const details = pkg.locator('.package-body > details');
    await expect(details).toHaveCount(2);
    for (const detail of await details.all()) {
      await expect(detail).toHaveAttribute('open', '');
    }
    await expect(pkg.locator('.package-body pre')).toBeVisible();
    await pkg.locator('.package-toggle').click();
    await expect(pkg.locator('.package-body')).toBeHidden();
    await pkg.locator('.package-toggle').click();
    await expect(pkg.locator('.package-body pre')).toBeVisible();
    await details.first().locator('summary').click();
    await page.getByRole('button', { name: 'Collapse all packages', exact: true }).click();
    await page.getByRole('button', { name: 'Expand all packages', exact: true }).click();
    await expect(details.first()).not.toHaveAttribute('open', '');
    await expect(details.last()).toHaveAttribute('open', '');
  }
});

test("module lookup ignores dependency paths named module inside blocks", async ({ page }) => {
  await page.goto(moduleBlockURL);
  await expect(page.getByText('Base package', { exact: true }).locator('..').locator('dd')).toHaveText('example.com/block');
  await expect(page.locator('.package-header h3')).toHaveText(['.', 'child']);
});

test("whole-path redaction cannot label a coverage file as the module root", async ({ page }) => {
  await page.goto(redactedCoverageURL);
  const table = page.locator('table').filter({ has: page.locator('th', { hasText: 'Statements' }) });
  const row = table.locator('tbody tr').filter({ hasText: '[REDACTED]' });
  await expect(row).toHaveCount(1);
  await expect(row.locator('td').first()).toHaveText('unavailable');
  await expect(row.locator('td').nth(1)).toHaveText('[REDACTED]');
  for (const button of await table.locator('thead button').all()) {
    await button.click();
    await expect(row.locator('td').first()).toHaveText('unavailable');
  }
  expect(await page.content()).not.toContain('example.com/sort/pkg2/file10.go');
});

test("package sorting uses natural names and exact metrics without losing state", async ({ page }) => {
  await page.goto(packagesURL);
  const select = page.getByRole('combobox', { name: 'Sort packages', exact: true });
  const names = async () => page.locator('#package-list > .package-row').evaluateAll(rows =>
    rows.map(row => row.dataset.package.split('/').at(-1)));
  expect(await names()).toEqual(['pkg1', 'pkg2', 'pkg3', 'pkg4', 'pkg10']);
  const pkg = page.locator('.package-row[data-package="example.com/sort/pkg2"]');
  await pkg.evaluate(row => { row.retainedIdentity = true; });
  await pkg.locator('.package-toggle').click();
  for (const [key, ascending, descending] of [
    ['name', ['pkg1', 'pkg2', 'pkg3', 'pkg4', 'pkg10'], ['pkg10', 'pkg4', 'pkg3', 'pkg2', 'pkg1']],
    ['status', ['pkg2', 'pkg4', 'pkg1', 'pkg3', 'pkg10'], ['pkg10', 'pkg1', 'pkg3', 'pkg4', 'pkg2']],
    ['coverage', ['pkg2', 'pkg10', 'pkg1', 'pkg3', 'pkg4'], ['pkg1', 'pkg10', 'pkg2', 'pkg3', 'pkg4']],
    ['duration', ['pkg3', 'pkg2', 'pkg10', 'pkg1', 'pkg4'], ['pkg1', 'pkg10', 'pkg2', 'pkg3', 'pkg4']],
  ]) {
    await select.selectOption(key);
    expect(await names()).toEqual(ascending);
    const reverse = page.getByRole('button', { name: 'Ascending order; sort packages descending', exact: true });
    await reverse.focus();
    await page.keyboard.press('Enter');
    expect(await names()).toEqual(descending);
    await expect(reverse).toHaveCount(0);
    await expect(page.locator('#package-sort-direction')).toHaveAccessibleName('Descending order; sort packages ascending');
    await expect(pkg.locator('.package-contents')).toBeHidden();
    expect(await pkg.evaluate(row => row.retainedIdentity)).toBe(true);
  }
  await expect(page.locator('#package-sort-status')).toHaveText('Packages sorted by Duration, descending');
  await page.getByRole('searchbox', { name: 'Test name', exact: true }).fill('TestPackage/child');
  await switchView(page, 'Nested');
  await page.getByRole('button', { name: 'Clear all filters', exact: true }).click();
  expect(await names()).toEqual(['pkg1', 'pkg10', 'pkg2', 'pkg3', 'pkg4']);
  await expect(pkg.locator('.package-contents')).toBeHidden();
});

test("printing collapsed packages reveals all evidence and restores their state", async ({ page }) => {
  await page.goto(packagesURL);
  await page.getByRole('button', { name: 'Collapse all packages', exact: true }).click();
  await page.getByRole('searchbox', { name: 'Package', exact: true }).fill('no-match');
  await page.emulateMedia({ media: 'print' });
  await page.evaluate(() => window.dispatchEvent(new Event('beforeprint')));
  await expect(page.locator('.package-row:visible')).toHaveCount(5);
  await expect(page.locator('.package-contents:visible')).toHaveCount(5);
  await expect(page.locator('.package-toggle:visible, #toggle-packages:visible, .package-sorting:visible')).toHaveCount(0);
  await expect(page.locator('details:not([open])')).toHaveCount(0);
  await page.evaluate(() => window.dispatchEvent(new Event('afterprint')));
  await page.emulateMedia({ media: 'screen' });
  await page.getByRole('button', { name: 'Clear all filters', exact: true }).click();
  await expect(page.locator('.package-contents:visible')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Expand all packages', exact: true })).toBeVisible();
});

test("sortable tables stay usable on narrow screens and preserve their order for print", async ({ page }, testInfo) => {
  await page.goto(sortingURL);
  const coverage = page.getByRole('table', { name: 'Weighted coverage by file', exact: true });
  for (const colorScheme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme });
    for (const width of [390, 320]) {
      await page.setViewportSize({ width, height: 844 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      const button = coverage.getByRole('button', { name: 'Sort by Coverage', exact: true });
      await button.click();
      await expect(coverage.locator('th[aria-sort]')).toHaveText('Coverage');
      const bounds = await button.boundingBox();
      expect(bounds.x).toBeGreaterThanOrEqual(0);
      expect(bounds.x + bounds.width).toBeLessThanOrEqual(width);
    }
    await page.screenshot({ path: testInfo.outputPath(`sorted-mobile-${colorScheme}.png`) });
  }
  const before = await coverage.locator('tbody').innerText();
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.emulateMedia({ media: 'print' });
  await page.evaluate(() => window.dispatchEvent(new Event('beforeprint')));
  await expect(coverage.locator('tbody tr:visible')).toHaveCount(6);
  expect(await coverage.locator('tbody').innerText()).toBe(before);
  await page.evaluate(() => window.dispatchEvent(new Event('afterprint')));
  await page.emulateMedia({ media: 'screen' });
  expect(await coverage.locator('tbody').innerText()).toBe(before);
});

for (const view of ["Flat", "Nested"]) {
  test(`independent filters combine with status in ${view} view`, async ({ page }) => {
    await page.goto(filterURL);
    await switchView(page, view);
    const packageFilter = page.getByRole("searchbox", { name: "Package", exact: true });
    const testFilter = page.getByRole("searchbox", { name: "Test name", exact: true });
    const outputFilter = page.getByRole("searchbox", { name: "Output", exact: true });
    const status = page.getByLabel("Status", { exact: true });
    const clear = page.getByRole("button", { name: "Clear all filters", exact: true });
    const count = page.locator("#match-count");

    // These tokens appear only in a different field, or in presentation metadata.
    for (const [field, value] of [
      [packageFilter, "example.com/decoy"], [packageFilter, "TestNameOnly"],
      [testFilter, "TestDecoy"], [testFilter, "example.com/alpha"],
      [outputFilter, "TestNameOnly"], [outputFilter, "duration unavailable"],
      [outputFilter, "metadata-only-token"], [outputFilter, "Test output"],
    ]) {
      await field.fill(value);
      await expect(count).toHaveText(`0 matching results · ${view} view`);
      await expect(page.locator(".filterable:visible")).toHaveCount(0);
      await clear.click();
    }

    await packageFilter.fill("  EXAMPLE.COM/ALPHA  ");
    await expect(count).toHaveText(`4 matching results · ${view} view`);
    await testFilter.fill("  LEAF[1]  ");
    await expect(count).toHaveText(`1 matching result · ${view} view`);
    await outputFilter.fill("  SHARED-OUTPUT  ");
    await status.selectOption("passed");
    await expect(count).toHaveText(`1 matching result · ${view} view`);
    const pkg = page.locator('.package-row[data-package="example.com/alpha"]');
    await expect(pkg.locator('[data-test="TestRoot/Leaf[1]"]')).toBeVisible();
    await expect(page.locator('.package-row[data-package="example.com/beta"]')).toBeHidden();
    await expect(pkg.locator('[data-test="TestNameOnly"]')).toBeHidden();
    if (view === "Nested") await expect(pkg.locator('[data-test="TestRoot"]')).toBeVisible();
    else await expect(pkg.locator('[data-test="TestRoot"]')).toBeHidden();

    await outputFilter.fill("literal.*");
    await expect(count).toHaveText(`1 matching result · ${view} view`);
    await outputFilter.fill("<img src=x onerror=alert(1)>");
    await expect(count).toHaveText(`1 matching result · ${view} view`);
    await expect(page.locator("img")).toHaveCount(0);
    await outputFilter.fill("parent-only transcript");
    await expect(count).toHaveText(`0 matching results · ${view} view`);
    await outputFilter.fill("shared-output");
    await status.selectOption("failed");
    await expect(count).toHaveText(`0 matching results · ${view} view`);
    await packageFilter.fill("beta");
    await expect(count).toHaveText(`1 matching result · ${view} view`);
    await switchView(page, view === "Flat" ? "Nested" : "Flat");
    await expect(packageFilter).toHaveValue("beta");
    await expect(testFilter).toHaveValue("  LEAF[1]  ");
    await expect(outputFilter).toHaveValue("shared-output");
    await expect(status).toHaveValue("failed");
    await expect(count).toContainText("1 matching result ·");
  });
}

test("clear all filters restores branches and output panels from the keyboard", async ({ page }) => {
  const id = await occurrence(page, "TestTree/group/pass").getAttribute("id");
  const output = page.locator(`#${id} > .test-content > details`);
  await output.locator("summary").click();
  await switchView(page, "Nested");
  const initialCount = await page.locator("#match-count").textContent();
  await page.getByRole("button", { name: "Collapse all", exact: true }).click();
  await page.getByRole("searchbox", { name: "Test name", exact: true }).fill("   ");
  await expect(page.locator("#match-count")).toHaveText(initialCount);
  await expect(output).toBeHidden();
  await expect(page.getByRole("button", { name: "Expand all", exact: true })).toBeEnabled();
  await page.getByRole("searchbox", { name: "Package", exact: true }).fill("tested-browser");
  await page.getByRole("searchbox", { name: "Test name", exact: true }).fill("group/pass");
  await page.getByRole("searchbox", { name: "Output", exact: true }).fill("passing output");
  await page.getByLabel("Status", { exact: true }).selectOption("passed");
  await expect(output).toBeVisible();
  const clear = page.getByRole("button", { name: "Clear all filters", exact: true });
  await page.getByRole("radio", { name: "Nested", exact: true }).focus();
  // Enter keyboard modality; WebKit's Tab order follows host preferences.
  await page.keyboard.press("Tab");
  await clear.focus();
  await expect(clear).toBeFocused();
  expect(await clear.evaluate(element => getComputedStyle(element).outlineStyle)).not.toBe("none");
  await page.keyboard.press("Enter");
  await expect(clear).toBeFocused();
  for (const name of ["Package", "Test name", "Output"]) {
    await expect(page.getByRole("searchbox", { name, exact: true })).toHaveValue("");
  }
  await expect(page.getByLabel("Status", { exact: true })).toHaveValue("");
  await expect(page.getByRole("radio", { name: "Nested", exact: true })).toBeChecked();
  await expect(page.locator("#match-count")).toHaveText(initialCount);
  await expect(output).toBeHidden();
  await expect(output).toHaveAttribute("open", "");
  await expect(page.getByRole("button", { name: "Expand all", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "Expand all", exact: true }).click();
  await expect(output.locator("pre")).toBeVisible();
  await page.getByRole("searchbox", { name: "Output", exact: true }).fill("no-such-result-98347");
  await expect(page.locator(".filterable:visible")).toHaveCount(0);
  await clear.click();
  await expect(page.locator("#match-count")).toHaveText(initialCount);
});

test("output filters include package, build, unattributed, and diagnostic evidence", async ({ page }) => {
  await page.goto(filterURL);
  const output = page.getByRole("searchbox", { name: "Output", exact: true });
  const packageFilter = page.getByRole("searchbox", { name: "Package", exact: true });
  const testFilter = page.getByRole("searchbox", { name: "Test name", exact: true });
  const diagnostic = page.locator('.build-card[data-status="incomplete"]')
    .filter({ hasText: "diagnostic-only-token" });
  for (const view of ["Flat", "Nested"]) {
    await switchView(page, view);
    await output.fill("package-only transcript");
    await expect(page.locator(".package-row:visible")).toHaveCount(2);
    await expect(page.locator(".test-row:visible")).toHaveCount(0);
    await expect(page.locator("#match-count")).toHaveText(`2 matching results · ${view} view`);
    for (const [term, row, matches] of [
      ["compiler-only-token", page.locator('.build-card[data-package="compiler/package"]'), 1],
      // Unknown actions retain both unattributed output and a diagnostic preview.
      ["unattributed-only-token", page.locator('[aria-labelledby="unattributed-heading"]'), 2],
      ["diagnostic-only-token", diagnostic, 1],
    ]) {
      await output.fill(term);
      await expect(row).toBeVisible();
      await expect(page.locator(".filterable:visible")).toHaveCount(matches);
      await expect(page.locator("#match-count")).toHaveText(
        `${matches} matching result${matches === 1 ? "" : "s"} · ${view} view`);
      await testFilter.fill("TestRoot");
      await expect(page.locator(".filterable:visible")).toHaveCount(0);
      await testFilter.fill("");
    }
    const message = await diagnostic.locator('p[data-filter-output]').textContent();
    await output.fill(message);
    await expect(page.locator(".filterable:visible")).toHaveCount(1);
    await packageFilter.fill("compiler/package");
    await expect(page.locator(".filterable:visible")).toHaveCount(0);
    await output.fill("compiler-only-token");
    await expect(page.locator(".filterable:visible")).toHaveCount(1);
    await page.getByLabel("Status", { exact: true }).selectOption("failed");
    await expect(page.locator(".filterable:visible")).toHaveCount(1);
    await page.getByRole("button", { name: "Clear all filters", exact: true }).click();
  }
});

test("name filters use redacted identities and match repeated occurrences", async ({ page }) => {
  const name = page.getByRole("searchbox", { name: "Test name", exact: true });
  await name.fill("secret-one");
  await expect(page.locator(".filterable:visible")).toHaveCount(0);
  await name.fill("[REDACTED]");
  await expect(page.locator(".test-row:visible")).toHaveCount(8);
  await expect(page.locator("#match-count")).toHaveText("8 matching results · Flat view");
  await name.fill("TestTree/group/fail");
  await expect(page.locator(".test-row:visible")).toHaveCount(2);
  await name.fill("attempt 2");
  await expect(page.locator(".filterable:visible")).toHaveCount(0);
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
  await page.getByRole("searchbox", { name: "Package", exact: true }).fill("tested-browser");
  await page.getByRole("searchbox", { name: "Test name", exact: true }).fill("group/skip");
  await page.getByRole("searchbox", { name: "Output", exact: true }).fill("skip");
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
  await page.getByRole("button", { name: "Clear all filters", exact: true }).click();
  await expect(occurrence(page, "TestTree/group/pass")).toBeHidden();
  await switchView(page, "Flat");
  await page.emulateMedia({ media: "print" });
  await expect(page.locator(".test-row:visible")).toHaveCount(total);
  await expect(page.locator(".branch-toggle:visible")).toHaveCount(0);
  await expect(page.locator("#report-controls")).toBeHidden();
});

for (const colorScheme of ["light", "dark"]) {
  test(`nested layout fits desktop, tablet, and mobile in ${colorScheme} mode`, async ({ page }, testInfo) => {
    await page.emulateMedia({ colorScheme });
    await switchView(page, "Nested");
    for (const viewport of [
      { width: 1440, height: 1000 }, { width: 900, height: 1000 },
      { width: 390, height: 844 }, { width: 320, height: 740 },
    ]) {
      await page.setViewportSize(viewport);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      const controls = page.locator('.toolbar input[type="search"], .toolbar select, .view-options, #clear-filters');
      const boxes = await controls.evaluateAll(elements => elements.map(element => {
        const { x, y, width, height } = element.getBoundingClientRect();
        return { x, y, width, height };
      }));
      expect(boxes).toHaveLength(6);
      for (const [i, box] of boxes.entries()) {
        expect(box.width).toBeGreaterThan(80);
        expect(box.x).toBeGreaterThanOrEqual(0);
        expect(box.x + box.width).toBeLessThanOrEqual(viewport.width);
        for (const other of boxes.slice(i + 1)) {
          expect(box.x + box.width <= other.x || other.x + other.width <= box.x ||
            box.y + box.height <= other.y || other.y + other.height <= box.y).toBe(true);
        }
      }
      for (const name of ["Flat", "Nested"]) {
        const box = await page.getByRole("radio", { name, exact: true }).boundingBox();
        expect(box.x).toBeGreaterThanOrEqual(0);
        expect(box.x + box.width).toBeLessThanOrEqual(viewport.width);
      }
      await page.locator("#report-controls").evaluate(toolbar => toolbar.scrollIntoView());
      await page.screenshot({ path: testInfo.outputPath(`nested-${colorScheme}-${viewport.width}.png`) });
    }
  });
}

test("JavaScript-disabled reports retain all flat evidence without inactive controls", async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false });
  try {
    const page = await context.newPage();
    await page.goto(reportURL);
    await expect(page.locator('.assessment-list > div').filter({ hasText: 'Base package' }).locator('dd')).toHaveText('example.com/tested-browser');
    await expect(page.locator('.package-header h3')).toHaveText('.');
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
    await page.goto(sortingURL);
    await expect(page.locator('.package-contents:visible')).toHaveCount(3);
    await expect(page.locator('.package-toggle:visible, #toggle-packages:visible, .package-sorting:visible')).toHaveCount(0);
    await expect(page.locator('.table-sort')).toHaveCount(0);
    for (const table of await page.locator('.sortable-table').all()) {
      await expect(table.locator('th[aria-sort="ascending"]')).toHaveText('Package');
      await expect(table.locator('tbody [data-package-label].sr-only')).toHaveCount(0);
      const packages = await table.locator('tbody tr').evaluateAll(rows =>
        rows.map(row => row.cells[0].textContent));
      expect(packages).toEqual([...packages].sort());
      for (const cell of await table.locator('tbody tr td:first-child').all()) {
        expect((await cell.innerText()).trim()).not.toBe('');
      }
    }
  } finally {
    await context.close();
  }
});

test("module-relative names retain full identity and every summary package cell", async ({ page }, testInfo) => {
  await page.goto(relativeURL);
  const assessment = page.locator('.assessment-list > div').filter({ has: page.locator('dt', { hasText: /^Base package$/ }) });
  await expect(assessment.locator('dd')).toHaveText('example.com/sort');
  const headers = page.locator('.package-header h3');
  await expect(headers).toHaveText(['.', 'example.com/sort-extra/pkg', 'other.example/pkg', 'pkg1', 'pkg2', 'pkg10']);
  await expect(headers.first()).toHaveAttribute('title', 'example.com/sort');
  await expect(page.locator('.package-row[data-package="example.com/sort"] .package-coverage')).toHaveText('Coverage 100.00%');
  await expect(page.locator('.package-row[data-package="example.com/sort/pkg2"] .package-coverage')).toHaveText('Coverage 10.91%');
  const filter = page.getByRole('searchbox', { name: 'Package', exact: true });
  for (const query of ['pkg2', 'example.com/sort/pkg2']) {
    await filter.fill(query);
    await expect(page.locator('.package-row:visible')).toHaveCount(1);
    await expect(page.locator('.package-row:visible h3')).toHaveText('pkg2');
    await expect(page.locator('.test-row:visible')).toHaveCount(2);
  }
  await page.getByRole('button', { name: 'Clear all filters', exact: true }).click();
  for (const table of await page.locator('.sortable-table').all()) {
    await expect(table.locator('tbody tr').first().locator('td').first()).toHaveText('.');
    for (const button of await table.locator('thead button').all()) {
      for (let direction = 0; direction < 2; direction++) {
        await button.click();
        await expect(table.locator('tbody [data-package-label].sr-only')).toHaveCount(0);
        for (const cell of await table.locator('tbody tr td:first-child').all()) {
          expect((await cell.innerText()).trim()).not.toBe('');
          await expect(cell.locator('[data-package-label]')).toBeVisible();
        }
      }
    }
  }
  await page.getByRole('button', { name: 'Collapse all packages', exact: true }).click();
  await page.locator('#packages-heading').scrollIntoViewIfNeeded();
  await page.screenshot({ path: testInfo.outputPath('relative-package-names.png'), fullPage: true });
});

test("test and coverage pages navigate to the local report index", async ({ page }) => {
  for (const url of [changesURL, new URL('coverage.html', changesURL).href, coverageFallbackURL]) {
    await page.goto(url);
    const link = page.getByRole('link', { name: '← Report index', exact: true });
    await expect(link).toHaveAttribute('href', 'index.html');
    await expect(link).toBeVisible();
    expect((await link.boundingBox()).y).toBeLessThan(60);
    await link.focus();
    await page.keyboard.press('Enter');
    await expect(page).toHaveURL(new URL('index.html', url).href);
    await expect(page.getByRole('link', { name: /Test report/ })).toBeVisible();
    await expect(page.getByRole('link', { name: /Coverage source/ })).toBeVisible();
  }
});

test("changed packages share the coverage baseline and preserve filter and disclosure state", async ({ page }, testInfo) => {
  await page.goto(changesURL);
  const toggle = page.getByRole('switch', { name: 'Changed packages only', exact: true });
  await expect(toggle).not.toBeChecked();
  await expect(page.locator('.test-visibility .tested-coverage-switch-count')).toHaveText('3');
  await expect(page.locator('.package-row:visible')).toHaveCount(6);
  const pkg = page.locator('.package-row[data-package="example.com/changes/changed"]');
  const child = pkg.locator('[data-test="TestValue/child"]').first();
  await child.locator('summary').click();
  await switchView(page, 'Nested');
  await pkg.locator('.branch-toggle').first().click();
  await toggle.focus();
  await page.keyboard.press('Space');
  await expect(toggle).toBeChecked();
  await expect(page.locator('.package-row:visible h3')).toHaveText(['changed', 'fresh', 'renamed']);
  await expect(page.locator('.test-row:visible')).toHaveCount(12);
  await expect(child.locator('pre')).toBeVisible();
  const packageFilter = page.getByRole('searchbox', { name: 'Package', exact: true });
  await packageFilter.fill('stable');
  await expect(page.locator('.package-row:visible')).toHaveCount(0);
  await packageFilter.fill('changed');
  await page.getByRole('searchbox', { name: 'Test name', exact: true }).fill('/child');
  await page.getByRole('searchbox', { name: 'Output', exact: true }).fill('retained child');
  await page.getByLabel('Status', { exact: true }).selectOption('passed');
  await expect(page.locator('#match-count')).toHaveText('2 matching results · Nested view');
  await pkg.locator('.package-toggle').click();
  await page.getByRole('combobox', { name: 'Sort packages', exact: true }).selectOption('duration');
  await page.getByRole('button', { name: 'Clear all filters', exact: true }).click();
  await expect(toggle).not.toBeChecked();
  await expect(page.locator('.package-row:visible')).toHaveCount(6);
  await expect(pkg.locator('.package-contents')).toBeHidden();
  await pkg.locator('.package-toggle').click();
  await expect(child).toBeHidden(); // Saved branch collapse survived filtering.
  await toggle.locator('..').click();
  await expect(toggle).toBeChecked();
  await expect(child.locator('pre')).toBeVisible();
  for (const colorScheme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme });
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: 1000 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await expect(toggle.locator('..')).toBeVisible();
      await page.screenshot({ path: testInfo.outputPath(`changed-packages-${colorScheme}-${width}.png`) });
    }
  }
  await page.emulateMedia({ media: 'print' });
  await page.evaluate(() => window.dispatchEvent(new Event('beforeprint')));
  await expect(page.locator('.test-row:visible')).toHaveCount(24);
  await expect(page.locator('#report-controls')).toBeHidden();
  await page.evaluate(() => window.dispatchEvent(new Event('afterprint')));
  await page.emulateMedia({ media: 'screen' });
  await expect(toggle).toBeChecked();
  await expect(page.locator('.test-row:visible')).toHaveCount(12);

  await page.goto(new URL('coverage.html', changesURL).href);
  const coverageToggle = page.getByRole('switch', { name: /Changed files only/ });
  await coverageToggle.locator('..').click();
  await expect(coverageToggle).toBeChecked();
  const fileNames = await page.locator('#files option:not([hidden]):not([disabled])').allTextContents();
  expect(fileNames).toHaveLength(3);
  await expect(page.locator('#files option[hidden][disabled]')).toHaveCount(3);
  expect(fileNames.join(' ')).toContain('/changed/');
  expect(fileNames.join(' ')).toContain('/fresh/');
  expect(fileNames.join(' ')).toContain('/renamed/');
});

test("change controls distinguish absent and unchanged baselines and degrade without scripts", async ({ page, browser }) => {
  await expect(page.locator('#changed-packages-only')).toHaveCount(0);
  await page.goto(unchangedURL);
  const toggle = page.getByRole('switch', { name: 'Changed packages only', exact: true });
  await expect(toggle).toBeDisabled();
  await expect(page.locator('.test-visibility .tested-coverage-switch-count')).toHaveText('0');
  await expect(page.locator('.package-row:visible')).toHaveCount(5);
  const context = await browser.newContext({ javaScriptEnabled: false });
  try {
    const fallback = await context.newPage();
    await fallback.goto(changesURL);
    await expect(fallback.locator('#changed-packages-only')).toBeHidden();
    await expect(fallback.locator('.test-row:visible')).toHaveCount(24);
    await expect(fallback.getByRole('link', { name: '← Report index', exact: true })).toBeVisible();
    await fallback.emulateMedia({ media: 'print' });
    await expect(fallback.locator('#changed-packages-only')).toBeHidden();
  } finally {
    await context.close();
  }
});

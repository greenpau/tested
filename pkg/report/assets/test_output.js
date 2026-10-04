/*
 * Copyright 2026 Paul Greenberg greenpau@outlook.com
 * Licensed under the Apache License, Version 2.0.
 */

(() => {
  "use strict";
  const filters = ["package", "test", "output"].map(name =>
    document.getElementById("filter-" + name));
  const clear = document.getElementById("clear-filters");
  const status = document.getElementById("status");
  const count = document.getElementById("match-count");
  const main = document.getElementById("report-content");
  const treeControls = document.getElementById("tree-controls");
  const expand = document.getElementById("expand-tests");
  const collapse = document.getElementById("collapse-tests");
  const rows = Array.from(document.querySelectorAll(".filterable"));
  const entries = new Map(rows.map((row) => {
    // Cache only this occurrence's evidence, before nesting adds descendants.
    const ownContent = row.classList.contains("package-card")
      ? row.querySelector(".package-body") : row;
    return [row, {
      row,
      packageName: (row.dataset.package || "").toLowerCase(),
      testName: (row.dataset.test || "").toLowerCase(),
      output: Array.from(ownContent.querySelectorAll("[data-filter-output]"),
        node => node.textContent.toLowerCase()),
      package: row.classList.contains("test-row")
        ? row.closest(".package-card") : null,
    }];
  }));
  const tests = Array.from(document.querySelectorAll(".test-row"));
  const byID = new Map(tests.map((row) => [row.id, row]));
  const branches = [];
  let nested = false;

  // Expanded static markup keeps all evidence printable without JavaScript,
  // including engines whose closed-details content cannot be revealed by CSS.
  for (const detail of document.querySelectorAll("details[data-collapse]")) {
    detail.open = false;
  }

  for (const row of tests) {
    const entry = entries.get(row);
    entry.list = row.parentElement;
    const parent = byID.get(row.dataset.parent);
    entry.parent = parent && entries.get(parent).package === entry.package
      ? parent : null;
    // The renderer emits parents before descendants in the original flat list.
    entry.depth = entry.parent ? entries.get(entry.parent).depth + 1 : 0;
    row.style.setProperty("--test-depth", Math.min(entry.depth, 6));
    entry.children = [];
    entry.collapsed = false;
  }
  for (const row of tests) {
    const entry = entries.get(row);
    if (entry.parent) entries.get(entry.parent).children.push(row);
  }
  for (const row of tests) {
    const entry = entries.get(row);
    if (!entry.children.length) continue;
    const group = document.createElement("div");
    group.className = "test-children";
    group.id = row.id + "-children";
    const button = document.createElement("button");
    button.type = "button";
    button.className = "branch-toggle";
    button.setAttribute("aria-controls", group.id);
    button.setAttribute("aria-describedby", row.id + "-name");
    button.hidden = true;
    button.addEventListener("click", () => {
      entry.collapsed = !entry.collapsed;
      apply();
    });
    row.querySelector(".test-heading").prepend(button);
    row.append(group);
    entry.group = group;
    entry.button = button;
    branches.push(entry);
  }

  const apply = () => {
    const [packageTerm, testTerm, outputTerm] = filters.map(input =>
      input.value.trim().toLowerCase());
    const state = status.value;
    const filtering = Boolean(packageTerm || testTerm || outputTerm || state);
    let matches = 0;
    for (const entry of entries.values()) {
      const matched = (!packageTerm || entry.packageName.includes(packageTerm)) &&
        (!testTerm || entry.testName.includes(testTerm)) &&
        (!outputTerm || entry.output.some(text => text.includes(outputTerm))) &&
        (!state || entry.row.dataset.status === state);
      entry.row.hidden = !matched;
      if (matched) matches++;
    }
    // Reverse source order visits every child before its ancestors, without
    // recursive walks or searching the increasingly large nested DOM.
    for (let i = rows.length - 1; i >= 0; i--) {
      const entry = entries.get(rows[i]);
      const parent = (nested && entry.parent) || entry.package;
      if (!entry.row.hidden && parent) parent.hidden = false;
    }
    for (const entry of branches) {
      const open = !entry.collapsed || filtering;
      entry.group.hidden = nested && !open;
      entry.button.hidden = !nested;
      entry.button.disabled = filtering;
      entry.button.setAttribute("aria-expanded", String(open));
      entry.button.textContent = open ? "▾" : "▸";
      entry.button.setAttribute("aria-label", (open ? "Hide " : "Show ") +
        entry.children.length + " subtest" +
        (entry.children.length === 1 ? "" : "s"));
    }
    treeControls.hidden = !nested || !branches.length;
    expand.disabled = collapse.disabled = filtering;
    count.textContent = matches + " matching result" +
      (matches === 1 ? "" : "s") + " · " +
      (nested ? "Nested" : "Flat") + " view";
  };

  for (const radio of document.querySelectorAll('input[name="test-view"]')) {
    radio.addEventListener("change", () => {
      if (!radio.checked) return;
      nested = radio.value === "nested";
      // Move, never clone: output and metadata occur once, and open details
      // survive both switches. Appending in source order restores flat order.
      for (const row of tests) {
        const entry = entries.get(row);
        const target = nested && entry.parent
          ? entries.get(entry.parent).group : entry.list;
        target.append(row);
      }
      main.dataset.view = nested ? "nested" : "flat";
      apply();
    });
  }
  expand.addEventListener("click", () => {
    for (const entry of branches) entry.collapsed = false;
    apply();
  });
  collapse.addEventListener("click", () => {
    for (const entry of branches) entry.collapsed = true;
    apply();
  });
  for (const input of filters) input.addEventListener("input", apply);
  status.addEventListener("change", apply);
  clear.addEventListener("click", () => {
    for (const input of filters) input.value = "";
    status.value = "";
    apply();
  });

  const collator = new Intl.Collator("en", { numeric: true, sensitivity: "base" });
  for (const table of document.querySelectorAll(".sortable-table")) {
    const headers = Array.from(table.querySelectorAll("thead th[data-sort]"));
    const body = table.tBodies[0];
    const records = Array.from(body.rows, (row, index) => ({
      row, index,
      values: headers.map((header, column) => {
        const cell = row.cells[column];
        if (header.dataset.sort === "number") {
          return cell.dataset.sortValue ? BigInt(cell.dataset.sortValue) : null;
        }
        if (header.dataset.sort === "ratio") {
          const total = BigInt(cell.dataset.sortTotal);
          return total ? [BigInt(cell.dataset.sortCovered), total] : null;
        }
        return cell.textContent.trim();
      }),
    }));
    const announcement = document.createElement("p");
    announcement.className = "sr-only";
    announcement.setAttribute("aria-live", "polite");
    table.after(announcement);
    const labels = headers.map(header => header.textContent);
    let active = 0;
    let direction = 1;
    const sort = () => {
      const type = headers[active].dataset.sort;
      records.sort((a, b) => {
        const left = a.values[active], right = b.values[active];
        // Unavailable evidence stays last in either direction. Compare exact
        // integers/ratios, never rounded percentages or formatted durations.
        if (left === null || right === null) {
          return left === right ? a.index - b.index : left === null ? 1 : -1;
        }
        let order;
        if (type === "text") order = collator.compare(left, right);
        else {
          const x = type === "ratio" ? left[0] * right[1] : left;
          const y = type === "ratio" ? right[0] * left[1] : right;
          order = x < y ? -1 : x > y ? 1 : 0;
        }
        return order * direction || a.index - b.index;
      });
      let previousPackage = null;
      for (const { row } of records) {
        const label = row.querySelector("[data-package-label]");
        label.classList.toggle("sr-only", label.textContent === previousPackage);
        previousPackage = label.textContent;
        body.append(row);
      }
      headers.forEach((header, column) => {
        if (column === active) header.setAttribute("aria-sort", direction === 1 ? "ascending" : "descending");
        else header.removeAttribute("aria-sort");
      });
    };
    headers.forEach((header, column) => {
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = labels[column];
      button.setAttribute("aria-label", "Sort by " + labels[column]);
      button.className = "table-sort";
      button.addEventListener("click", () => {
        direction = active === column ? -direction : 1;
        active = column;
        sort();
        announcement.textContent = labels[column] + ", " +
          (direction === 1 ? "ascending" : "descending");
      });
      header.replaceChildren(button);
    });
    sort();
    // Equal values retain the initial visible (natural package) order.
    records.forEach((record, index) => { record.index = index; });
  }

  // Native details need opening for print in browsers that suppress their
  // closed content independently of display rules. Restore the screen state.
  let closedDetails = [];
  window.addEventListener("beforeprint", () => {
    closedDetails = Array.from(document.querySelectorAll("details:not([open])"));
    for (const detail of closedDetails) detail.open = true;
  });
  window.addEventListener("afterprint", () => {
    for (const detail of closedDetails) detail.open = false;
    closedDetails = [];
  });
  apply();
  document.getElementById("report-controls").hidden = false;
})();

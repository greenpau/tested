/*
 * Copyright 2026 Paul Greenberg greenpau@outlook.com
 * Licensed under the Apache License, Version 2.0.
 */

(function () {
  "use strict";

  var MAX_CODE_UNITS = 8 * 1024 * 1024;
  var MAX_DOCUMENT_CODE_UNITS = 128 * 1024 * 1024;
  var MAX_SOURCE_LINES = 100000;
  var MAX_DIFF_FILES = 100000;
  var MAX_DIFF_HUNKS = 100000;
  var MAX_DIFF_LINES = 1000000;
  var MAX_RUNS = 250000;
  var CONTEXT_LINES = 3;
  var DIFF_SCHEMA = "tested/coverage-diff/v1";

  function onReady() {
    try {
      addReportNavigation();
      initialize();
    } catch (ignored) {
      // The Go coverage page remains fully functional when enhancement fails.
    }
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", onReady, false);
  } else {
    window.setTimeout(onReady, 0);
  }

  function addReportNavigation() {
    if (!document.body || document.getElementById("tested-report-navigation")) {
      return;
    }
    var nav = createElement("nav", "report-navigation coverage-navigation");
    nav.id = "tested-report-navigation";
    nav.setAttribute("aria-label", "Report navigation");
    var link = createElement("a", "", "← Report index");
    link.setAttribute("href", "index.html");
    nav.appendChild(link);
    document.body.insertBefore(nav, document.body.firstChild);
  }

  function initialize() {
    if (document.getElementById("tested-coverage-explorer")) {
      return;
    }

    var page = inspectCanonicalPage();
    if (!page) {
      return;
    }

    var embeddedDiff = readEmbeddedDiff();
    var ui = buildInterface(page);
    ui.changesTab.hidden = !embeddedDiff.valid;
    var changedSummary = summarizeChangedRecords(
      page,
      embeddedDiff.files
    );
    ui.changedOnlyField.hidden = !embeddedDiff.valid;
    ui.changedOnly.disabled = changedSummary.files === 0;
    ui.changedOnlyCount.textContent = String(changedSummary.files);
    if (changedSummary.files === 0) {
      ui.changedOnlyField.title = "No changed covered files";
    }
    var state = {
      page: page,
      ui: ui,
      diffFiles: embeddedDiff.files,
      baseCommit: embeddedDiff.baseCommit,
      hasBaselinePayload: embeddedDiff.valid,
      changedOnly: false,
      changedFileCount: changedSummary.files,
      changedPackageCount: changedSummary.packages,
      diffNotice: embeddedDiff.notice,
      currentDiffNotice: "",
      sourceCache: {id: "", value: null},
      diffCache: {id: "", value: null},
      expansions: new Map(),
      activeView: "coverage",
      coverageLines: "all",
      changesScope: "changes",
      layout: "unified",
      printAll: false,
      currentExpansionKey: "",
      currentGapCount: 0,
      currentFirstGapStart: null,
      pendingFocusIndex: null,
      pendingFocusNode: null,
      pendingAnnouncement: ""
    };

    commitInterface(page, ui);
    bindInterface(state);
    applyFileFilters(state, "");
    renderSelectedFile(state);
  }

  function inspectCanonicalPage() {
    var nav = document.getElementById("nav");
    var content = document.getElementById("content");
    var files = document.getElementById("files");

    if (!nav || !content || !files) {
      return null;
    }
    if (nav.tagName !== "DIV" || content.tagName !== "DIV" ||
        files.tagName !== "SELECT" || !isDescendant(nav, files)) {
      return null;
    }
    if (!files.options || files.options.length < 1 ||
        files.options.length > MAX_SOURCE_LINES) {
      return null;
    }

    var records = [];
    var recordsByID = new Map();
    var packageSet = new Map();
    for (var i = 0; i < files.options.length; i += 1) {
      var option = files.options[i];
      var expectedID = "file" + String(i);
      if (option.value !== expectedID || recordsByID.has(expectedID)) {
        return null;
      }

      var pre = document.getElementById(expectedID);
      if (!pre || pre.tagName !== "PRE" || pre.parentNode !== content ||
          !hasClass(pre, "file")) {
        return null;
      }

      var label = parseGoOptionLabel(option.textContent || "");
      if (!label) {
        return null;
      }

      var record = {
        id: expectedID,
        option: option,
        pre: pre,
        name: label.name,
        packageName: label.packageName,
        fileName: label.fileName,
        percent: label.percent
      };
      records.push(record);
      recordsByID.set(expectedID, record);
      packageSet.set(label.packageName, true);
    }

    var packages = [];
    packageSet.forEach(function (_, packageName) {
      packages.push(packageName);
    });
    packages.sort();

    return {
      nav: nav,
      content: content,
      files: files,
      records: records,
      recordsByID: recordsByID,
      packages: packages
    };
  }

  function parseGoOptionLabel(text) {
    var match = /^(.+) \(([0-9]+(?:\.[0-9]+)?)%\)$/.exec(text);
    if (!match) {
      return null;
    }

    var percent = Number(match[2]);
    if (!Number.isFinite(percent) || percent < 0 || percent > 100) {
      return null;
    }

    var name = match[1];
    var slash = name.lastIndexOf("/");
    if (slash === name.length - 1) {
      return null;
    }

    return {
      name: name,
      packageName: slash < 0 ? "(root)" : name.slice(0, slash),
      fileName: slash < 0 ? name : name.slice(slash + 1),
      percent: match[2] + "%"
    };
  }

  function isDescendant(parent, child) {
    var node = child;
    while (node) {
      if (node === parent) {
        return true;
      }
      node = node.parentNode;
    }
    return false;
  }

  function hasClass(node, className) {
    if (!node || typeof node.className !== "string") {
      return false;
    }
    var names = node.className.split(/\s+/);
    for (var i = 0; i < names.length; i += 1) {
      if (names[i] === className) {
        return true;
      }
    }
    return false;
  }

  function readEmbeddedDiff() {
    var result = {
      files: new Map(),
      baseCommit: "",
      valid: false,
      notice: ""
    };
    var node = document.getElementById("tested-coverage-data-v1");
    if (!node) {
      result.notice = "No source comparison was embedded.";
      return result;
    }
    if (node.tagName !== "SCRIPT") {
      result.notice = "The embedded source comparison is invalid.";
      return result;
    }

    var source = node.textContent || "";
    if (source.length < 1 || source.length > MAX_DOCUMENT_CODE_UNITS) {
      result.notice = "The embedded source comparison exceeds safe limits.";
      return result;
    }

    var decoded;
    try {
      decoded = JSON.parse(source);
    } catch (ignored) {
      result.notice = "The embedded source comparison is invalid.";
      return result;
    }

    var validated = validateDiffDocument(decoded);
    if (!validated) {
      result.notice = "The embedded source comparison is invalid.";
      return result;
    }
    result.files = validated.files;
    result.baseCommit = validated.baseCommit;
    result.valid = true;
    return result;
  }

  function validateDiffDocument(documentValue) {
    if (!isObject(documentValue) || documentValue.schema !== DIFF_SCHEMA ||
        typeof documentValue.base_commit !== "string" ||
        !/^(?:[0-9a-f]{40}|[0-9a-f]{64})$/.test(
          documentValue.base_commit
        ) ||
        !Array.isArray(documentValue.files) ||
        documentValue.files.length > MAX_DIFF_FILES) {
      return null;
    }

    var files = new Map();
    var lineCount = 0;
    var hunkCount = 0;
    for (var i = 0; i < documentValue.files.length; i += 1) {
      var value = documentValue.files[i];
      if (!isObject(value)) {
        return null;
      }
      var profilePath = value.profile_path;
      var oldPath = value.old_path === undefined ? "" : value.old_path;
      var newPath = value.new_path === undefined ? "" : value.new_path;
      var currentSHA256 = value.current_sha256;
      var reason = value.reason === undefined ? "" : value.reason;
      var hunks = value.hunks === undefined ? [] : value.hunks;
      if (!isBoundedString(profilePath) || profilePath.length < 1 ||
          !isBoundedString(oldPath) ||
          !isBoundedString(newPath) ||
          !isBoundedString(reason) ||
          !isKnownDiffStatus(value.status) ||
          !Array.isArray(hunks) || files.has(profilePath)) {
        return null;
      }
      if (value.status === "unavailable") {
        if (currentSHA256 !== undefined && currentSHA256 !== "") {
          return null;
        }
        currentSHA256 = "";
      } else if (typeof currentSHA256 !== "string" ||
          !/^[0-9a-f]{64}$/.test(currentSHA256)) {
        return null;
      }

      var file = {
        name: profilePath,
        oldPath: oldPath,
        newPath: newPath,
        currentSHA256: currentSHA256,
        status: value.status,
        reason: reason,
        hunks: []
      };
      var codeUnits = profilePath.length + oldPath.length +
        newPath.length + currentSHA256.length +
        value.status.length + reason.length;

      var previousOldEnd = 0;
      var previousNewEnd = 0;
      for (var h = 0; h < hunks.length; h += 1) {
        hunkCount += 1;
        if (hunkCount > MAX_DIFF_HUNKS) {
          return null;
        }
        var hunkValue = hunks[h];
        if (!isObject(hunkValue) ||
            !isNonnegativeInteger(hunkValue.old_start) ||
            !isNonnegativeInteger(hunkValue.old_lines) ||
            !isNonnegativeInteger(hunkValue.new_start) ||
            !isNonnegativeInteger(hunkValue.new_lines) ||
            !Array.isArray(hunkValue.lines)) {
          return null;
        }
        if ((hunkValue.old_lines > 0 && hunkValue.old_start < 1) ||
            (hunkValue.new_lines > 0 && hunkValue.new_start < 1)) {
          return null;
        }

        var oldEnd = hunkValue.old_start + hunkValue.old_lines;
        var newEnd = hunkValue.new_start + hunkValue.new_lines;
        if (h > 0 &&
            (hunkValue.old_start < previousOldEnd ||
             hunkValue.new_start < previousNewEnd)) {
          return null;
        }

        var hunk = {
          oldStart: hunkValue.old_start,
          oldLines: hunkValue.old_lines,
          newStart: hunkValue.new_start,
          newLines: hunkValue.new_lines,
          lines: []
        };
        var countedOld = 0;
        var countedNew = 0;
        for (var n = 0; n < hunkValue.lines.length; n += 1) {
          var lineValue = hunkValue.lines[n];
          if (!isObject(lineValue) || typeof lineValue.text !== "string" ||
              lineValue.text.indexOf("\n") !== -1 ||
              (lineValue.no_newline !== undefined &&
               typeof lineValue.no_newline !== "boolean")) {
            return null;
          }
          var normalizedText = normalizeComparedLine(lineValue.text);
          if (normalizedText === null) {
            return null;
          }

          var kind = normalizeDiffKind(lineValue.kind);
          if (!kind) {
            return null;
          }
          if (kind === "context") {
            countedOld += 1;
            countedNew += 1;
          } else if (kind === "delete") {
            countedOld += 1;
          } else {
            countedNew += 1;
          }

          lineCount += 1;
          codeUnits += normalizedText.length;
          if (lineCount > MAX_DIFF_LINES || codeUnits > MAX_CODE_UNITS) {
            return null;
          }
          hunk.lines.push({
            kind: kind,
            text: normalizedText,
            noNewline: lineValue.no_newline === true
          });
        }
        if (countedOld !== hunk.oldLines || countedNew !== hunk.newLines) {
          return null;
        }

        previousOldEnd = oldEnd;
        previousNewEnd = newEnd;
        file.hunks.push(hunk);
      }
      if (codeUnits > MAX_CODE_UNITS) {
        return null;
      }
      files.set(file.name, file);
    }
    return {
      baseCommit: documentValue.base_commit,
      files: files
    };
  }

  function isObject(value) {
    return value !== null && typeof value === "object" &&
      !Array.isArray(value);
  }

  function isBoundedString(value) {
    return typeof value === "string" && value.length <= MAX_CODE_UNITS;
  }

  function isNonnegativeInteger(value) {
    return Number.isSafeInteger(value) && value >= 0 &&
      value <= MAX_DIFF_LINES;
  }

  function isKnownDiffStatus(value) {
    return value === "unchanged" || value === "modified" ||
      value === "added" || value === "renamed" ||
      value === "untracked" || value === "unavailable";
  }

  function normalizeComparedLine(value) {
    var text = value;
    var firstCarriageReturn = text.indexOf("\r");
    if (firstCarriageReturn !== -1) {
      if (firstCarriageReturn !== text.length - 1) {
        return null;
      }
      text = text.slice(0, -1);
    }
    if (text.length > MAX_CODE_UNITS) {
      return null;
    }
    var expandedLength = text.length;
    var tab = text.indexOf("\t");
    while (tab !== -1) {
      expandedLength += 7;
      if (expandedLength > MAX_CODE_UNITS) {
        return null;
      }
      tab = text.indexOf("\t", tab + 1);
    }
    return text.replace(/\t/g, "        ");
  }

  function normalizeDiffKind(value) {
    if (value === "context" || value === " ") {
      return "context";
    }
    if (value === "add" || value === "+") {
      return "add";
    }
    if (value === "delete" || value === "-") {
      return "delete";
    }
    return "";
  }

  function buildInterface(page) {
    var root = createElement("section", "tested-coverage-explorer");
    root.id = "tested-coverage-explorer";
    root.setAttribute("aria-label", "Coverage explorer");

    var toolbar = createElement("div", "tested-coverage-toolbar");
    var filters = createElement("div", "tested-coverage-filters");
    var packageSelect = createElement("select", "");
    packageSelect.id = "tested-coverage-package";
    packageSelect.appendChild(createOption("", "All packages"));
    for (var i = 0; i < page.packages.length; i += 1) {
      packageSelect.appendChild(
        createOption(page.packages[i], page.packages[i])
      );
    }
    filters.appendChild(createField("Package", packageSelect));

    var fileField = createElement("label", "tested-coverage-field");
    var fileLabel = createElement(
      "span",
      "tested-coverage-field-label",
      "File"
    );
    fileField.appendChild(fileLabel);
    filters.appendChild(fileField);

    var changedOnlyField = createElement(
      "div",
      "tested-coverage-field tested-coverage-changed-filter"
    );
    changedOnlyField.hidden = true;
    changedOnlyField.appendChild(
      createElement("span", "tested-coverage-field-label", "Visibility")
    );
    var changedOnlyLabel = createElement(
      "label",
      "tested-coverage-switch"
    );
    var changedOnly = createElement("input", "");
    changedOnly.id = "tested-coverage-changed-only";
    changedOnly.type = "checkbox";
    changedOnly.disabled = true;
    changedOnly.setAttribute("role", "switch");
    changedOnly.setAttribute(
      "aria-controls",
      "tested-coverage-package files"
    );
    changedOnlyLabel.appendChild(changedOnly);
    var changedOnlyTrack = createElement(
      "span",
      "tested-coverage-switch-track"
    );
    changedOnlyTrack.setAttribute("aria-hidden", "true");
    changedOnlyLabel.appendChild(changedOnlyTrack);
    changedOnlyLabel.appendChild(
      createElement(
        "span",
        "tested-coverage-switch-text",
        "Changed files only"
      )
    );
    var changedOnlyCount = createElement(
      "span",
      "tested-coverage-switch-count",
      "0"
    );
    changedOnlyLabel.appendChild(changedOnlyCount);
    changedOnlyField.appendChild(changedOnlyLabel);
    filters.appendChild(changedOnlyField);

    var tabs = createElement("div", "tested-coverage-tabs");
    tabs.id = "tested-coverage-tabs";
    tabs.setAttribute("role", "tablist");
    tabs.setAttribute("aria-label", "Coverage display");
    var coverageTab = createActionButton(
      "tested-coverage-tab-coverage",
      "tested-coverage-tab",
      "Coverage",
      "show-coverage"
    );
    coverageTab.setAttribute("role", "tab");
    coverageTab.setAttribute("aria-selected", "true");
    coverageTab.setAttribute("aria-controls", "tested-coverage-view");
    coverageTab.tabIndex = 0;
    var changesTab = createActionButton(
      "tested-coverage-tab-changes",
      "tested-coverage-tab",
      "Changes",
      "show-changes"
    );
    changesTab.setAttribute("role", "tab");
    changesTab.setAttribute("aria-selected", "false");
    changesTab.setAttribute("aria-controls", "tested-coverage-view");
    changesTab.tabIndex = -1;
    tabs.appendChild(coverageTab);
    tabs.appendChild(changesTab);

    var options = createElement("div", "tested-coverage-options");
    var coverageControls = createElement(
      "div",
      "tested-coverage-mode-controls"
    );
    coverageControls.id = "tested-coverage-coverage-controls";
    var coverageLines = createElement("select", "");
    coverageLines.id = "tested-coverage-lines";
    coverageLines.appendChild(createOption("all", "All"));
    coverageLines.appendChild(
      createOption("uncovered", "Uncovered regions")
    );
    coverageControls.appendChild(createField("Coverage lines", coverageLines));

    var changesControls = createElement(
      "div",
      "tested-coverage-mode-controls"
    );
    changesControls.id = "tested-coverage-changes-controls";
    changesControls.hidden = true;
    var changesScope = createElement("select", "");
    changesScope.id = "tested-coverage-scope";
    changesScope.appendChild(createOption("changes", "Changes only"));
    changesScope.appendChild(createOption("entire", "Entire file"));
    changesControls.appendChild(createField("Changes scope", changesScope));

    var layout = createElement("div", "tested-coverage-field");
    layout.id = "tested-coverage-layout";
    var layoutLabel = createElement(
      "span",
      "tested-coverage-field-label",
      "Layout"
    );
    var layoutButtons = createElement(
      "div",
      "tested-coverage-layout-buttons"
    );
    layoutButtons.setAttribute("role", "group");
    layoutButtons.setAttribute("aria-label", "Change layout");
    var unifiedButton = createActionButton(
      "tested-coverage-layout-unified",
      "tested-coverage-layout-button",
      "Unified",
      "layout-unified"
    );
    unifiedButton.setAttribute("aria-pressed", "true");
    var splitButton = createActionButton(
      "tested-coverage-layout-split",
      "tested-coverage-layout-button",
      "Split",
      "layout-split"
    );
    splitButton.setAttribute("aria-pressed", "false");
    layoutButtons.appendChild(unifiedButton);
    layoutButtons.appendChild(splitButton);
    layout.appendChild(layoutLabel);
    layout.appendChild(layoutButtons);
    changesControls.appendChild(layout);

    var expandAll = createActionButton(
      "tested-coverage-expand-all",
      "tested-coverage-button",
      "Expand all gaps",
      "expand-all"
    );
    expandAll.disabled = true;

    options.appendChild(coverageControls);
    options.appendChild(changesControls);
    options.appendChild(expandAll);
    toolbar.appendChild(filters);
    toolbar.appendChild(tabs);
    toolbar.appendChild(options);

    var status = createElement("div", "tested-coverage-status");
    status.id = "tested-coverage-status";
    status.setAttribute("role", "status");
    status.setAttribute("aria-live", "polite");
    status.setAttribute("aria-atomic", "true");

    root.appendChild(toolbar);
    root.appendChild(status);

    var view = createElement("div", "tested-coverage-view");
    view.id = "tested-coverage-view";
    view.setAttribute("role", "tabpanel");
    view.setAttribute(
      "aria-labelledby",
      "tested-coverage-tab-coverage"
    );
    view.setAttribute("aria-live", "off");

    return {
      root: root,
      fileField: fileField,
      packageSelect: packageSelect,
      changedOnlyField: changedOnlyField,
      changedOnly: changedOnly,
      changedOnlyCount: changedOnlyCount,
      coverageTab: coverageTab,
      changesTab: changesTab,
      coverageControls: coverageControls,
      changesControls: changesControls,
      coverageLines: coverageLines,
      changesScope: changesScope,
      unifiedButton: unifiedButton,
      splitButton: splitButton,
      expandAll: expandAll,
      status: status,
      view: view
    };
  }

  function createElement(tagName, className, text) {
    var node = document.createElement(tagName);
    if (className) {
      node.className = className;
    }
    if (text !== undefined) {
      node.textContent = text;
    }
    return node;
  }

  function createOption(value, text) {
    var option = createElement("option", "", text);
    option.value = value;
    return option;
  }

  function createField(label, control) {
    var field = createElement("label", "tested-coverage-field");
    field.appendChild(
      createElement("span", "tested-coverage-field-label", label)
    );
    field.appendChild(control);
    return field;
  }

  function createActionButton(id, className, text, action) {
    var button = createElement("button", className, text);
    button.id = id;
    button.type = "button";
    button.setAttribute("data-tested-action", action);
    return button;
  }

  function commitInterface(page, ui) {
    page.nav.appendChild(ui.root);
    ui.fileField.appendChild(page.files);
    page.content.appendChild(ui.view);
    if (document.body) {
      addClass(document.body, "tested-coverage-enhanced");
    }
  }

  function addClass(node, className) {
    if (!hasClass(node, className)) {
      node.className = node.className ?
        node.className + " " + className : className;
    }
  }

  function bindInterface(state) {
    state.ui.root.addEventListener("change", function (event) {
      try {
        handleChange(state, event.target);
      } catch (ignored) {
        showCanonicalFallback(
          state,
          selectedRecord(state),
          "Coverage explorer could not update this file."
        );
      }
    }, false);
    state.ui.root.addEventListener("click", function (event) {
      try {
        handleClick(state, event.target);
      } catch (ignored) {
        showCanonicalFallback(
          state,
          selectedRecord(state),
          "Coverage explorer could not update this file."
        );
      }
    }, false);
    state.ui.view.addEventListener("click", function (event) {
      try {
        handleClick(state, event.target);
      } catch (ignored) {
        showCanonicalFallback(
          state,
          selectedRecord(state),
          "Coverage explorer could not update this file."
        );
      }
    }, false);
    state.ui.root.addEventListener("keydown", function (event) {
      try {
        handleTabKey(state, event);
      } catch (ignored) {
        // Native button keyboard behavior remains available.
      }
    }, false);
    window.addEventListener("hashchange", function () {
      synchronizeHash(state);
    }, false);
    window.addEventListener("beforeprint", function () {
      if (!state.printAll) {
        state.printAll = true;
        renderSelectedFile(state);
      }
    }, false);
    window.addEventListener("afterprint", function () {
      if (state.printAll) {
        state.printAll = false;
        renderSelectedFile(state);
      }
    }, false);
  }

  function handleChange(state, target) {
    if (target === state.ui.packageSelect) {
      applyFileFilters(state, target.value);
      return;
    }
    if (target === state.ui.changedOnly) {
      state.changedOnly = target.checked &&
        state.changedFileCount > 0;
      target.checked = state.changedOnly;
      if (!applyFileFilters(
        state,
        state.ui.packageSelect.value
      )) {
        renderSelectedFile(state);
      }
      return;
    }
    if (target === state.page.files) {
      renderSelectedFile(state);
      return;
    }
    if (target === state.ui.coverageLines) {
      if (target.value === "all" || target.value === "uncovered") {
        state.coverageLines = target.value;
        renderSelectedFile(state);
      }
      return;
    }
    if (target === state.ui.changesScope) {
      if (target.value === "changes" || target.value === "entire") {
        state.changesScope = target.value;
        renderSelectedFile(state);
      }
    }
  }

  function handleClick(state, target) {
    var button = findActionButton(state.ui.root, target);
    if (!button || button.disabled) {
      return;
    }

    var action = button.getAttribute("data-tested-action");
    if (action === "show-coverage") {
      state.activeView = "coverage";
      renderSelectedFile(state);
    } else if (action === "show-changes") {
      state.activeView = "changes";
      renderSelectedFile(state);
    } else if (action === "layout-unified") {
      state.layout = "unified";
      renderSelectedFile(state);
    } else if (action === "layout-split") {
      state.layout = "split";
      renderSelectedFile(state);
    } else if (action === "expand-gap") {
      expandGap(
        state,
        button.getAttribute("data-tested-gap"),
        button.getAttribute("data-tested-start"),
        button.getAttribute("data-tested-end")
      );
    } else if (action === "expand-all") {
      expandAllGaps(state);
    }
  }

  function findActionButton(root, target) {
    var node = target;
    while (node && node !== root) {
      if (node.tagName === "BUTTON" &&
          node.hasAttribute("data-tested-action")) {
        return node;
      }
      node = node.parentNode;
    }
    return null;
  }

  function handleTabKey(state, event) {
    if (event.target !== state.ui.coverageTab &&
        event.target !== state.ui.changesTab) {
      return;
    }
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight" &&
        event.key !== "Home" && event.key !== "End") {
      return;
    }

    var showChanges = !state.ui.changesTab.hidden &&
      !state.ui.changesTab.disabled &&
      (event.key === "ArrowRight" || event.key === "End");
    state.activeView = showChanges ? "changes" : "coverage";
    event.preventDefault();
    renderSelectedFile(state);
    if (showChanges) {
      state.ui.changesTab.focus();
    } else {
      state.ui.coverageTab.focus();
    }
  }

  function summarizeChangedRecords(page, diffFiles) {
    var packages = new Map();
    var files = 0;
    for (var i = 0; i < page.records.length; i += 1) {
      if (isChangedDiffFile(diffFiles.get(page.records[i].name))) {
        files += 1;
        packages.set(page.records[i].packageName, true);
      }
    }
    return {
      files: files,
      packages: packages.size
    };
  }

  function isChangedDiffFile(file) {
    return !!file && (
      file.status === "modified" ||
      file.status === "added" ||
      file.status === "renamed" ||
      file.status === "untracked"
    );
  }

  function isChangedRecord(state, record) {
    return isChangedDiffFile(state.diffFiles.get(record.name));
  }

  function applyFileFilters(state, packageName) {
    var current = state.page.recordsByID.get(state.page.files.value);
    var visiblePackages = new Map();
    for (var i = 0; i < state.page.records.length; i += 1) {
      var candidate = state.page.records[i];
      if (!state.changedOnly || isChangedRecord(state, candidate)) {
        visiblePackages.set(candidate.packageName, true);
      }
    }
    if (packageName !== "" && !visiblePackages.has(packageName)) {
      packageName = "";
    }
    for (var optionIndex = 0;
      optionIndex < state.ui.packageSelect.options.length;
      optionIndex += 1) {
      var packageOption = state.ui.packageSelect.options[optionIndex];
      var packageAvailable = packageOption.value === "" ||
        visiblePackages.has(packageOption.value);
      packageOption.hidden = !packageAvailable;
      packageOption.disabled = !packageAvailable;
    }

    var first = null;
    var currentMatches = false;
    for (var recordIndex = 0;
      recordIndex < state.page.records.length;
      recordIndex += 1) {
      var record = state.page.records[recordIndex];
      var matches = (!state.changedOnly || isChangedRecord(state, record)) &&
        (packageName === "" || record.packageName === packageName);
      record.option.hidden = !matches;
      record.option.disabled = !matches;
      if (matches && !first) {
        first = record;
      }
      if (matches && current && record.id === current.id) {
        currentMatches = true;
      }
    }

    state.ui.packageSelect.value = packageName;
    state.ui.changedOnly.checked = state.changedOnly;
    if (!first) {
      return false;
    }
    if (!currentMatches) {
      state.page.files.value = first.id;
      dispatchNativeChange(state.page.files);
      return true;
    }
    return false;
  }

  function dispatchNativeChange(node) {
    var event = document.createEvent("Event");
    event.initEvent("change", true, false);
    node.dispatchEvent(event);
  }

  function synchronizeHash(state) {
    var hash = window.location.hash || "";
    if (!/^#file(?:0|[1-9][0-9]*)$/.test(hash)) {
      return;
    }
    var id = hash.slice(1);
    var record = state.page.recordsByID.get(id);
    if (!record || state.page.files.value === id) {
      return;
    }
    var clearsChangedOnly = state.changedOnly &&
      !isChangedRecord(state, record);
    if (clearsChangedOnly) {
      state.changedOnly = false;
      state.ui.changedOnly.checked = false;
    }
    if ((state.ui.packageSelect.value &&
        state.ui.packageSelect.value !== record.packageName) ||
        clearsChangedOnly) {
      applyFileFilters(state, "");
    }
    state.page.files.value = id;
    dispatchNativeChange(state.page.files);
  }

  function selectedRecord(state) {
    return state.page.recordsByID.get(state.page.files.value) || null;
  }

  function renderSelectedFile(state) {
    var record = selectedRecord(state);
    if (!record) {
      return;
    }

    state.currentDiffNotice = "";
    state.pendingFocusNode = null;
    hideCanonicalFiles(state.page);
    state.ui.view.hidden = false;
    var source = getSource(state, record);
    if (!source.ok) {
      showCanonicalFallback(
        state,
        record,
        "This file exceeds the coverage explorer's safe rendering limits."
      );
      return;
    }

    var diff = getDiff(state, record, source);
    state.currentDiffNotice = diff.ok ? "" : diff.reason;
    if (state.activeView === "changes" && !diff.ok) {
      state.activeView = "coverage";
    }
    updateControls(state, diff.ok);
    clearNode(state.ui.view);

    state.currentGapCount = 0;
    state.currentFirstGapStart = null;
    if (state.activeView === "changes") {
      renderChanges(state, record, source, diff.model);
    } else {
      renderCoverage(state, record, source);
    }
    state.ui.expandAll.disabled = state.currentGapCount === 0;
    completePendingFocus(state);
  }

  function hideCanonicalFiles(page) {
    for (var i = 0; i < page.records.length; i += 1) {
      page.records[i].pre.style.display = "none";
    }
  }

  function showCanonicalFallback(state, record, message) {
    state.activeView = "coverage";
    state.currentDiffNotice = "";
    hideCanonicalFiles(state.page);
    clearNode(state.ui.view);
    state.ui.view.hidden = true;
    state.currentGapCount = 0;
    state.currentFirstGapStart = null;
    state.pendingFocusIndex = null;
    state.pendingFocusNode = null;
    state.pendingAnnouncement = "";
    state.ui.expandAll.disabled = true;
    updateControls(state, false);
    if (record) {
      record.pre.style.display = "block";
    }
    setStatus(state, message + " Showing canonical Go coverage.");
  }

  function updateControls(state, diffAvailable) {
    var coverageActive = state.activeView === "coverage";
    state.ui.coverageTab.setAttribute(
      "aria-selected",
      coverageActive ? "true" : "false"
    );
    state.ui.changesTab.setAttribute(
      "aria-selected",
      coverageActive ? "false" : "true"
    );
    state.ui.changesTab.hidden = !state.hasBaselinePayload;
    state.ui.coverageTab.tabIndex = coverageActive ? 0 : -1;
    state.ui.changesTab.tabIndex = coverageActive ? -1 : 0;
    state.ui.view.setAttribute(
      "aria-labelledby",
      coverageActive ?
        "tested-coverage-tab-coverage" :
        "tested-coverage-tab-changes"
    );
    state.ui.changesTab.disabled =
      !state.hasBaselinePayload || !diffAvailable;
    state.ui.changedOnlyField.hidden = !state.hasBaselinePayload;
    state.ui.changedOnly.disabled =
      !state.hasBaselinePayload || state.changedFileCount === 0;
    if (diffAvailable) {
      state.ui.changesTab.removeAttribute("title");
    } else {
      state.ui.changesTab.title = state.currentDiffNotice ||
        "No validated source comparison for this file";
    }
    state.ui.coverageControls.hidden = !coverageActive;
    state.ui.changesControls.hidden = coverageActive;
    state.ui.unifiedButton.setAttribute(
      "aria-pressed",
      state.layout === "unified" ? "true" : "false"
    );
    state.ui.splitButton.setAttribute(
      "aria-pressed",
      state.layout === "split" ? "true" : "false"
    );
  }

  function clearNode(node) {
    while (node.firstChild) {
      node.removeChild(node.firstChild);
    }
  }

  function getSource(state, record) {
    if (state.sourceCache.id === record.id && state.sourceCache.value) {
      return state.sourceCache.value;
    }

    var result;
    try {
      result = tokenizeSource(record.pre);
    } catch (ignored) {
      result = {ok: false};
    }
    state.sourceCache = {id: record.id, value: result};
    return result;
  }

  function tokenizeSource(pre) {
    if (!validateSourceTree(pre)) {
      return {ok: false};
    }

    var lines = [newSourceLine()];
    var runCount = 0;
    var codeUnits = 0;
    var lastCharacter = "";
    var walker = document.createTreeWalker(pre, 4, null);
    var textNode = walker.nextNode();
    while (textNode) {
      var annotation = findCoverageAnnotation(textNode, pre);
      var text = textNode.nodeValue || "";
      codeUnits += text.length;
      if (codeUnits > MAX_CODE_UNITS) {
        return {ok: false};
      }
      if (text.length > 0) {
        lastCharacter = text.charAt(text.length - 1);
      }

      var offset = 0;
      while (offset <= text.length) {
        var newline = text.indexOf("\n", offset);
        var end = newline === -1 ? text.length : newline;
        if (annotation.coverage !== null &&
            (end > offset || newline !== -1)) {
          lines[lines.length - 1].tracked = true;
          if (annotation.coverage === 0) {
            lines[lines.length - 1].uncovered = true;
          }
        }
        if (end > offset) {
          if (appendRun(
            lines[lines.length - 1].runs,
            text.slice(offset, end),
            annotation
          )) {
            runCount += 1;
            if (runCount > MAX_RUNS) {
              return {ok: false};
            }
          }
        }
        if (newline === -1) {
          break;
        }
        lines.push(newSourceLine());
        if (lines.length > MAX_SOURCE_LINES + 1) {
          return {ok: false};
        }
        offset = newline + 1;
      }
      textNode = walker.nextNode();
    }

    var endsWithNewline = codeUnits > 0 && lastCharacter === "\n";
    if (endsWithNewline && lines.length > 0 &&
        lines[lines.length - 1].runs.length === 0) {
      lines.pop();
    }
    if (codeUnits === 0) {
      lines = [];
    }
    if (lines.length > MAX_SOURCE_LINES) {
      return {ok: false};
    }

    var plainLines = [];
    var uncovered = [];
    for (var i = 0; i < lines.length; i += 1) {
      var plainParts = [];
      for (var r = 0; r < lines[i].runs.length; r += 1) {
        plainParts.push(lines[i].runs[r].text);
      }
      plainLines.push(plainParts.join(""));
      if (lines[i].uncovered) {
        uncovered.push(i);
      }
    }

    return {
      ok: true,
      lines: lines,
      plainLines: plainLines,
      uncovered: uncovered,
      codeUnits: codeUnits,
      runCount: runCount,
      endsWithNewline: endsWithNewline
    };
  }

  function newSourceLine() {
    return {
      runs: [],
      tracked: false,
      uncovered: false
    };
  }

  var SHA256_INITIAL = [
    0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
    0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19
  ];
  var SHA256_CONSTANTS = [
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5,
    0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
    0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
    0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
    0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
    0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3,
    0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5,
    0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
    0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
  ];

  function sha256CanonicalSource(source) {
    var state = newSHA256State();
    for (var i = 0; i < source.plainLines.length; i += 1) {
      var line = source.plainLines[i];
      if (!sha256UpdateCanonicalText(state, line)) {
        return "";
      }
      var hasLineFeed = i + 1 < source.plainLines.length ||
        source.endsWithNewline;
      if (hasLineFeed &&
          (line.length === 0 || line.charAt(line.length - 1) !== "\r")) {
        sha256PushByte(state, 0x0a);
      }
    }
    return sha256Finish(state);
  }

  function newSHA256State() {
    return {
      hash: SHA256_INITIAL.slice(),
      block: new Uint8Array(64),
      words: new Uint32Array(64),
      blockLength: 0,
      byteLength: 0
    };
  }

  function sha256UpdateCanonicalText(state, text) {
    for (var i = 0; i < text.length; i += 1) {
      var first = text.charCodeAt(i);
      if (first === 0x09) {
        for (var space = 0; space < 8; space += 1) {
          sha256PushByte(state, 0x20);
        }
        continue;
      }
      if (first === 0x0d) {
        sha256PushByte(state, 0x0a);
        continue;
      }
      if (first < 0x80) {
        sha256PushByte(state, first);
        continue;
      }
      if (first < 0x800) {
        sha256PushByte(state, 0xc0 | (first >>> 6));
        sha256PushByte(state, 0x80 | (first & 0x3f));
        continue;
      }
      if (first >= 0xd800 && first <= 0xdbff) {
        if (i + 1 >= text.length) {
          return false;
        }
        var second = text.charCodeAt(i + 1);
        if (second < 0xdc00 || second > 0xdfff) {
          return false;
        }
        var codePoint = 0x10000 +
          ((first - 0xd800) << 10) + (second - 0xdc00);
        sha256PushByte(state, 0xf0 | (codePoint >>> 18));
        sha256PushByte(state, 0x80 | ((codePoint >>> 12) & 0x3f));
        sha256PushByte(state, 0x80 | ((codePoint >>> 6) & 0x3f));
        sha256PushByte(state, 0x80 | (codePoint & 0x3f));
        i += 1;
        continue;
      }
      if (first >= 0xdc00 && first <= 0xdfff) {
        return false;
      }
      sha256PushByte(state, 0xe0 | (first >>> 12));
      sha256PushByte(state, 0x80 | ((first >>> 6) & 0x3f));
      sha256PushByte(state, 0x80 | (first & 0x3f));
    }
    return true;
  }

  function sha256PushByte(state, value) {
    state.block[state.blockLength] = value;
    state.blockLength += 1;
    state.byteLength += 1;
    if (state.blockLength === 64) {
      sha256ProcessBlock(state);
      state.blockLength = 0;
    }
  }

  function sha256ProcessBlock(state) {
    var words = state.words;
    var block = state.block;
    for (var i = 0; i < 16; i += 1) {
      var offset = i * 4;
      words[i] = (
        (block[offset] << 24) |
        (block[offset + 1] << 16) |
        (block[offset + 2] << 8) |
        block[offset + 3]
      ) >>> 0;
    }
    for (var word = 16; word < 64; word += 1) {
      var x = words[word - 15];
      var y = words[word - 2];
      var sigma0 = (
        rotateRight(x, 7) ^ rotateRight(x, 18) ^ (x >>> 3)
      ) >>> 0;
      var sigma1 = (
        rotateRight(y, 17) ^ rotateRight(y, 19) ^ (y >>> 10)
      ) >>> 0;
      words[word] = (
        words[word - 16] + sigma0 + words[word - 7] + sigma1
      ) >>> 0;
    }

    var a = state.hash[0];
    var b = state.hash[1];
    var c = state.hash[2];
    var d = state.hash[3];
    var e = state.hash[4];
    var f = state.hash[5];
    var g = state.hash[6];
    var h = state.hash[7];
    for (var round = 0; round < 64; round += 1) {
      var sum1 = (
        rotateRight(e, 6) ^ rotateRight(e, 11) ^ rotateRight(e, 25)
      ) >>> 0;
      var choose = ((e & f) ^ ((~e) & g)) >>> 0;
      var temporary1 = (
        h + sum1 + choose + SHA256_CONSTANTS[round] + words[round]
      ) >>> 0;
      var sum0 = (
        rotateRight(a, 2) ^ rotateRight(a, 13) ^ rotateRight(a, 22)
      ) >>> 0;
      var majority = ((a & b) ^ (a & c) ^ (b & c)) >>> 0;
      var temporary2 = (sum0 + majority) >>> 0;
      h = g;
      g = f;
      f = e;
      e = (d + temporary1) >>> 0;
      d = c;
      c = b;
      b = a;
      a = (temporary1 + temporary2) >>> 0;
    }

    state.hash[0] = (state.hash[0] + a) >>> 0;
    state.hash[1] = (state.hash[1] + b) >>> 0;
    state.hash[2] = (state.hash[2] + c) >>> 0;
    state.hash[3] = (state.hash[3] + d) >>> 0;
    state.hash[4] = (state.hash[4] + e) >>> 0;
    state.hash[5] = (state.hash[5] + f) >>> 0;
    state.hash[6] = (state.hash[6] + g) >>> 0;
    state.hash[7] = (state.hash[7] + h) >>> 0;
  }

  function rotateRight(value, count) {
    return ((value >>> count) | (value << (32 - count))) >>> 0;
  }

  function sha256Finish(state) {
    var messageBytes = state.byteLength;
    sha256PushByte(state, 0x80);
    while (state.blockLength !== 56) {
      sha256PushByte(state, 0);
    }
    var highBits = Math.floor(messageBytes / 0x20000000) >>> 0;
    var lowBits = (messageBytes * 8) >>> 0;
    sha256PushByte(state, (highBits >>> 24) & 0xff);
    sha256PushByte(state, (highBits >>> 16) & 0xff);
    sha256PushByte(state, (highBits >>> 8) & 0xff);
    sha256PushByte(state, highBits & 0xff);
    sha256PushByte(state, (lowBits >>> 24) & 0xff);
    sha256PushByte(state, (lowBits >>> 16) & 0xff);
    sha256PushByte(state, (lowBits >>> 8) & 0xff);
    sha256PushByte(state, lowBits & 0xff);

    var digest = "";
    for (var i = 0; i < state.hash.length; i += 1) {
      digest += ("00000000" + state.hash[i].toString(16)).slice(-8);
    }
    return digest;
  }

  function validateSourceTree(pre) {
    var stack = [];
    if (pre.firstChild) {
      stack.push(pre.firstChild);
    }
    while (stack.length > 0) {
      var node = stack.pop();
      if (node.nextSibling) {
        stack.push(node.nextSibling);
      }
      if (node.nodeType === 1) {
        if (node.tagName !== "SPAN" || !coverageClass(node)) {
          return false;
        }
        if (node.firstChild) {
          stack.push(node.firstChild);
        }
        continue;
      }
      if (node.nodeType !== 3) {
        return false;
      }
    }
    return true;
  }

  function findCoverageAnnotation(textNode, pre) {
    var node = textNode.parentNode;
    while (node && node !== pre) {
      var value = coverageClass(node);
      if (value) {
        return {
          coverage: value.coverage,
          className: value.className,
          title: node.getAttribute("title") || ""
        };
      }
      node = node.parentNode;
    }
    return {
      coverage: null,
      className: "",
      title: ""
    };
  }

  function coverageClass(node) {
    if (!node || typeof node.className !== "string") {
      return null;
    }
    var names = node.className.split(/\s+/);
    for (var i = 0; i < names.length; i += 1) {
      var match = /^cov(10|[0-9])$/.exec(names[i]);
      if (match) {
        return {
          coverage: Number(match[1]),
          className: names[i]
        };
      }
    }
    return null;
  }

  function appendRun(runs, text, annotation) {
    if (!text) {
      return false;
    }
    var previous = runs.length > 0 ? runs[runs.length - 1] : null;
    if (previous && previous.coverage === annotation.coverage &&
        previous.title === annotation.title) {
      previous.text += text;
      return false;
    }
    runs.push({
      text: text,
      coverage: annotation.coverage,
      className: annotation.className,
      title: annotation.title
    });
    return true;
  }

  function getDiff(state, record, source) {
    if (state.diffCache.id === record.id && state.diffCache.value) {
      return state.diffCache.value;
    }

    var data = state.diffFiles.get(record.name);
    if (!data) {
      var missing = {
        ok: false,
        reason: state.diffFiles.size === 0 ?
          (state.diffNotice ? state.diffNotice + " " : "") +
            "Run with --coverage-diff-base <revision> to compare source changes." :
          "No source comparison is available for this file."
      };
      state.diffCache = {id: record.id, value: missing};
      return missing;
    }
    if (data.status === "unavailable") {
      var unavailable = {
        ok: false,
        reason: data.reason ||
          "This covered source could not be compared."
      };
      state.diffCache = {id: record.id, value: unavailable};
      return unavailable;
    }

    var currentSHA256 = sha256CanonicalSource(source);
    if (!currentSHA256 || currentSHA256 !== data.currentSHA256) {
      var integrityFailure = {
        ok: false,
        reason: "The embedded comparison does not match the current source digest."
      };
      state.diffCache = {id: record.id, value: integrityFailure};
      return integrityFailure;
    }

    var model = buildDiffModel(data, source);
    var result = model ?
      {ok: true, model: model} :
      {
        ok: false,
        reason: "The comparison does not match this covered source."
      };
    state.diffCache = {id: record.id, value: result};
    return result;
  }

  function buildDiffModel(data, source) {
    var rows = [];
    var sourceLine = 1;
    var oldLine = 1;
    var added = 0;
    var deleted = 0;

    for (var h = 0; h < data.hunks.length; h += 1) {
      var hunk = data.hunks[h];
      var newAnchor = hunk.newLines === 0 ?
        hunk.newStart + 1 : hunk.newStart;
      var oldAnchor = hunk.oldLines === 0 ?
        hunk.oldStart + 1 : hunk.oldStart;
      var newGap = newAnchor - sourceLine;
      var oldGap = oldAnchor - oldLine;
      if (newGap < 0 || oldGap < 0 || newGap !== oldGap ||
          sourceLine + newGap > source.plainLines.length + 1) {
        return null;
      }

      for (var before = 0; before < newGap; before += 1) {
        var beforeIndex = sourceLine - 1;
        rows.push({
          kind: "context",
          text: source.plainLines[beforeIndex],
          oldNo: oldLine,
          newNo: sourceLine,
          sourceIndex: beforeIndex,
          noNewline: false
        });
        sourceLine += 1;
        oldLine += 1;
      }

      var hunkOldLine = oldAnchor;
      var hunkNewLine = newAnchor;
      for (var n = 0; n < hunk.lines.length; n += 1) {
        var line = hunk.lines[n];
        if (line.kind === "context") {
          if (!matchesCurrentLine(source, hunkNewLine, line.text)) {
            return null;
          }
          rows.push({
            kind: "context",
            text: line.text,
            oldNo: hunkOldLine,
            newNo: hunkNewLine,
            sourceIndex: hunkNewLine - 1,
            noNewline: line.noNewline
          });
          hunkOldLine += 1;
          hunkNewLine += 1;
        } else if (line.kind === "delete") {
          rows.push({
            kind: "delete",
            text: line.text,
            oldNo: hunkOldLine,
            newNo: null,
            sourceIndex: null,
            noNewline: line.noNewline
          });
          hunkOldLine += 1;
          deleted += 1;
        } else {
          if (!matchesCurrentLine(source, hunkNewLine, line.text)) {
            return null;
          }
          rows.push({
            kind: "add",
            text: line.text,
            oldNo: null,
            newNo: hunkNewLine,
            sourceIndex: hunkNewLine - 1,
            noNewline: line.noNewline
          });
          hunkNewLine += 1;
          added += 1;
        }
      }
      if (hunkOldLine !== oldAnchor + hunk.oldLines ||
          hunkNewLine !== newAnchor + hunk.newLines) {
        return null;
      }
      sourceLine = hunkNewLine;
      oldLine = hunkOldLine;
    }

    while (sourceLine <= source.plainLines.length) {
      var index = sourceLine - 1;
      rows.push({
        kind: "context",
        text: source.plainLines[index],
        oldNo: oldLine,
        newNo: sourceLine,
        sourceIndex: index,
        noNewline: false
      });
      sourceLine += 1;
      oldLine += 1;
    }
    if (rows.length > MAX_SOURCE_LINES + MAX_DIFF_LINES) {
      return null;
    }

    return {
      data: data,
      rows: rows,
      splitRows: null,
      added: added,
      deleted: deleted
    };
  }

  function matchesCurrentLine(source, lineNumber, text) {
    return lineNumber >= 1 && lineNumber <= source.plainLines.length &&
      source.plainLines[lineNumber - 1] === text;
  }

  function renderCoverage(state, record, source) {
    var fileView = createElement(
      "section",
      "tested-coverage-file-view tested-coverage-coverage-view"
    );
    fileView.setAttribute("aria-label", "Coverage for " + record.name);

    if (!state.printAll && state.coverageLines === "uncovered" &&
        source.uncovered.length === 0) {
      fileView.appendChild(
        createElement(
          "p",
          "tested-coverage-empty",
          "No uncovered regions in this file."
        )
      );
      state.ui.view.appendChild(fileView);
      state.currentExpansionKey = coverageExpansionKey(state, record);
      setStatus(
        state,
        record.fileName + " · " + record.percent +
          " coverage · no uncovered regions"
      );
      return;
    }

    var table = createElement(
      "div",
      "tested-coverage-code-table tested-coverage-unified"
    );
    table.setAttribute("role", "table");
    table.setAttribute("aria-label", "Annotated source");
    appendTableHeader(table, [
      "Current line number",
      "Current source"
    ]);

    var visibility = null;
    if (!state.printAll && state.coverageLines === "uncovered") {
      visibility = visibilityAround(
        source.lines.length,
        source.uncovered,
        CONTEXT_LINES
      );
    }
    var expansionKey = coverageExpansionKey(state, record);
    state.currentExpansionKey = expansionKey;
    appendSegmentedRows(
      table,
      source.lines.length,
      visibility,
      getExpansion(state, expansionKey),
      function (index) {
        appendRenderedRow(
          state,
          table,
          renderCoverageLine(source.lines[index], index),
          index
        );
      },
      function (start, end, gapKey) {
        table.appendChild(renderGap(state, start, end, gapKey, false));
      },
      null
    );

    fileView.appendChild(table);
    state.ui.view.appendChild(fileView);
    setStatus(
      state,
      record.fileName + " · " + record.percent + " coverage · " +
        String(source.uncovered.length) + " uncovered " +
        pluralize(source.uncovered.length, "line", "lines")
    );
  }

  function coverageExpansionKey(state, record) {
    return record.id + "|coverage|" + state.coverageLines;
  }

  function renderCoverageLine(line, index) {
    var className = "tested-coverage-code-line";
    if (line.uncovered) {
      className += " tested-coverage-line-uncovered";
    } else if (line.tracked) {
      className += " tested-coverage-line-covered";
    } else {
      className += " tested-coverage-line-untracked";
    }

    var row = createElement("div", className);
    row.setAttribute("role", "row");
    row.appendChild(createLineNumber(
      index + 1,
      "new",
      "Current line " + String(index + 1)
    ));
    var code = createElement("code", "tested-coverage-code");
    code.setAttribute("role", "cell");
    appendSemanticCue(
      code,
      line.uncovered ? "Uncovered current line. " :
        (line.tracked ? "Covered current line. " :
          "Untracked current line. ")
    );
    appendAnnotatedRuns(code, line.runs);
    row.appendChild(code);
    return row;
  }

  function renderChanges(state, record, source, model) {
    var fileView = createElement(
      "section",
      "tested-coverage-file-view tested-coverage-changes-view"
    );
    fileView.setAttribute("aria-label", "Changes for " + record.name);

    var rows;
    if (state.layout === "split") {
      if (!model.splitRows) {
        model.splitRows = buildSplitRows(model.rows);
      }
      rows = model.splitRows;
    } else {
      rows = model.rows;
    }

    var changed = [];
    for (var i = 0; i < rows.length; i += 1) {
      if (isChangedRow(rows[i], state.layout)) {
        changed.push(i);
      }
    }
    if (!state.printAll && state.changesScope === "changes" &&
        changed.length === 0) {
      fileView.appendChild(
        createElement(
          "p",
          "tested-coverage-empty",
          "No changed lines in this file."
        )
      );
      state.ui.view.appendChild(fileView);
      state.currentExpansionKey = changesExpansionKey(state, record);
      setChangesStatus(state, record, model);
      return;
    }

    var tableClass = "tested-coverage-code-table ";
    tableClass += state.layout === "split" ?
      "tested-coverage-split" : "tested-coverage-unified";
    var table = createElement("div", tableClass);
    table.setAttribute("role", "table");
    table.setAttribute(
      "aria-label",
      state.layout === "split" ?
        "Side-by-side source changes" : "Unified source changes"
    );
    if (state.layout === "split") {
      appendTableHeader(table, [
        "Baseline source",
        "Current source"
      ]);
    } else {
      appendTableHeader(table, [
        "Baseline line number",
        "Current line number",
        "Change type",
        "Source"
      ]);
    }

    var visibility = null;
    if (!state.printAll && state.changesScope === "changes") {
      visibility = visibilityAround(rows.length, changed, CONTEXT_LINES);
    }
    var expansionKey = changesExpansionKey(state, record);
    state.currentExpansionKey = expansionKey;
    appendSegmentedRows(
      table,
      rows.length,
      visibility,
      getExpansion(state, expansionKey),
      function (index) {
        if (state.layout === "split") {
          appendRenderedRow(
            state,
            table,
            renderSplitRow(rows[index], source),
            index
          );
        } else {
          appendRenderedRow(
            state,
            table,
            renderUnifiedDiffRow(rows[index], source),
            index
          );
        }
      },
      function (start, end, gapKey) {
        table.appendChild(
          renderGap(state, start, end, gapKey, state.layout === "split")
        );
      },
      function (start, end) {
        return diffGapIdentity(rows, start, end, state.layout);
      }
    );

    fileView.appendChild(table);
    state.ui.view.appendChild(fileView);
    setChangesStatus(state, record, model);
  }

  function changesExpansionKey(state, record) {
    return record.id + "|changes|" + state.changesScope;
  }

  function setChangesStatus(state, record, model) {
    var base = state.baseCommit ?
      " · Base " + state.baseCommit.slice(0, 12) + " → working source" : "";
    var fileState = model.data.status;
    if (fileState === "renamed" && model.data.oldPath &&
        model.data.newPath) {
      fileState = "renamed " + model.data.oldPath + " → " +
        model.data.newPath;
    }
    setStatus(
      state,
      record.fileName + " · " + fileState + " · " +
        String(model.added) + " " +
        pluralize(model.added, "addition", "additions") + " · " +
        String(model.deleted) + " " +
        pluralize(model.deleted, "deletion", "deletions") + " · " +
        (state.layout === "split" ? "split view" : "unified view") +
        base
    );
  }

  function isChangedRow(row, layout) {
    if (layout === "split") {
      return (row.left && row.left.kind === "delete") ||
        (row.right && row.right.kind === "add");
    }
    return row.kind === "add" || row.kind === "delete";
  }

  function renderUnifiedDiffRow(row, source) {
    var className = "tested-coverage-code-line tested-coverage-diff-" +
      row.kind;
    var node = createElement("div", className);
    node.setAttribute("role", "row");
    node.appendChild(createLineNumber(
      row.oldNo,
      "old",
      row.oldNo === null ?
        "No baseline line" : "Baseline line " + String(row.oldNo)
    ));
    node.appendChild(createLineNumber(
      row.newNo,
      "new",
      row.newNo === null ?
        "No current line" : "Current line " + String(row.newNo)
    ));

    var marker = createDiffMarker(
      row.kind,
      row.kind === "add" ? "Added line" :
        (row.kind === "delete" ? "Deleted line" : "Unchanged line")
    );
    node.appendChild(marker);

    var code = createElement("code", "tested-coverage-code");
    code.setAttribute("role", "cell");
    appendSemanticCue(code, unifiedSemanticCue(row.kind));
    if (row.sourceIndex !== null) {
      appendAnnotatedRuns(code, source.lines[row.sourceIndex].runs);
    } else {
      code.appendChild(document.createTextNode(row.text));
    }
    appendNoNewline(code, row.noNewline);
    node.appendChild(code);
    return node;
  }

  function buildSplitRows(rows) {
    var split = [];
    var index = 0;
    while (index < rows.length) {
      if (rows[index].kind === "context") {
        split.push({left: rows[index], right: rows[index]});
        index += 1;
        continue;
      }

      var deleted = [];
      var added = [];
      while (index < rows.length && rows[index].kind !== "context") {
        if (rows[index].kind === "delete") {
          deleted.push(rows[index]);
        } else {
          added.push(rows[index]);
        }
        index += 1;
      }
      var count = Math.max(deleted.length, added.length);
      for (var i = 0; i < count; i += 1) {
        split.push({
          left: i < deleted.length ? deleted[i] : null,
          right: i < added.length ? added[i] : null
        });
      }
    }
    return split;
  }

  function renderSplitRow(row, source) {
    var node = createElement("div", "tested-coverage-split-row");
    node.setAttribute("role", "row");
    node.appendChild(renderSplitCell(row.left, "left", source));
    node.appendChild(renderSplitCell(row.right, "right", source));
    return node;
  }

  function renderSplitCell(line, side, source) {
    if (!line) {
      var empty = createElement(
        "div",
        "tested-coverage-split-cell tested-coverage-split-" + side +
          " tested-coverage-split-empty"
      );
      empty.setAttribute("role", "cell");
      empty.setAttribute(
        "aria-label",
        side === "left" ?
          "No corresponding baseline line" :
          "No corresponding current line"
      );
      return empty;
    }

    var className = "tested-coverage-split-cell tested-coverage-split-" +
      side + " tested-coverage-diff-" + line.kind;
    var cell = createElement("div", className);
    cell.setAttribute("role", "cell");
    cell.appendChild(
      createLineNumber(
        side === "left" ? line.oldNo : line.newNo,
        side === "left" ? "old" : "new"
      )
    );
    var marker = createDiffMarker(line.kind, "");
    cell.appendChild(marker);

    var code = createElement("code", "tested-coverage-code");
    appendSemanticCue(code, splitSemanticCue(line, side));
    if (side === "right" && line.sourceIndex !== null) {
      appendAnnotatedRuns(code, source.lines[line.sourceIndex].runs);
    } else {
      code.appendChild(document.createTextNode(line.text));
    }
    appendNoNewline(code, line.noNewline);
    cell.appendChild(code);
    return cell;
  }

  function appendTableHeader(table, labels) {
    table.setAttribute("aria-colcount", String(labels.length));
    var row = createElement(
      "div",
      "tested-coverage-table-header sr-only"
    );
    row.setAttribute("role", "row");
    for (var i = 0; i < labels.length; i += 1) {
      var header = createElement("span", "", labels[i]);
      header.setAttribute("role", "columnheader");
      row.appendChild(header);
    }
    table.appendChild(row);
  }

  function appendSemanticCue(parent, text) {
    parent.appendChild(createElement("span", "sr-only", text));
  }

  function unifiedSemanticCue(kind) {
    if (kind === "add") {
      return "Added to current source. ";
    }
    if (kind === "delete") {
      return "Deleted from baseline source. ";
    }
    return "Unchanged in baseline and current source. ";
  }

  function splitSemanticCue(line, side) {
    if (side === "left") {
      return "Baseline line " + String(line.oldNo) + ", " +
        (line.kind === "delete" ?
          "deleted from baseline source. " :
          "unchanged baseline source. ");
    }
    return "Current line " + String(line.newNo) + ", " +
      (line.kind === "add" ?
        "added to current source. " :
        "unchanged current source. ");
  }

  function createLineNumber(number, side, accessibleLabel) {
    var className = "tested-coverage-line-number " +
      "tested-coverage-line-number-" + side;
    var value = number === null ? "" : String(number);
    var node = createElement("span", className);
    var visible = createElement("span", "", value);
    visible.setAttribute("aria-hidden", "true");
    node.appendChild(visible);
    if (accessibleLabel) {
      node.setAttribute("role", "cell");
      node.setAttribute("aria-label", accessibleLabel);
    } else {
      node.setAttribute("aria-hidden", "true");
    }
    return node;
  }

  function createDiffMarker(kind, accessibleLabel) {
    var marker = createElement("span", "tested-coverage-diff-marker");
    var visible = createElement(
      "span",
      "",
      kind === "add" ? "+" : (kind === "delete" ? "−" : " ")
    );
    visible.setAttribute("aria-hidden", "true");
    marker.appendChild(visible);
    if (accessibleLabel) {
      marker.setAttribute("role", "cell");
      marker.setAttribute("aria-label", accessibleLabel);
    } else {
      marker.setAttribute("aria-hidden", "true");
    }
    return marker;
  }

  function appendAnnotatedRuns(parent, runs) {
    for (var i = 0; i < runs.length; i += 1) {
      var run = runs[i];
      if (run.coverage === null) {
        parent.appendChild(document.createTextNode(run.text));
      } else {
        var span = createElement(
          "span",
          "tested-coverage-run " + run.className,
          run.text
        );
        if (run.title) {
          span.title = run.title;
        }
        parent.appendChild(span);
      }
    }
  }

  function appendNoNewline(parent, noNewline) {
    if (!noNewline) {
      return;
    }
    parent.appendChild(
      createElement(
        "span",
        "tested-coverage-no-newline",
        " No newline at end of file"
      )
    );
  }

  function appendRenderedRow(state, parent, row, index) {
    if (state.pendingFocusIndex === index && !state.pendingFocusNode) {
      row.tabIndex = -1;
      state.pendingFocusNode = row;
    }
    parent.appendChild(row);
  }

  function completePendingFocus(state) {
    if (state.pendingFocusIndex === null) {
      return;
    }
    var focusNode = state.pendingFocusNode;
    var announcement = state.pendingAnnouncement;
    state.pendingFocusIndex = null;
    state.pendingFocusNode = null;
    state.pendingAnnouncement = "";
    if (announcement) {
      state.ui.status.textContent += " " + announcement;
    }
    if (focusNode && typeof focusNode.focus === "function") {
      try {
        focusNode.focus();
      } catch (ignored) {
        // The expanded content remains visible when focus is unavailable.
      }
    }
  }

  function visibilityAround(length, focus, context) {
    var visible = new Array(length);
    for (var i = 0; i < length; i += 1) {
      visible[i] = false;
    }
    for (var n = 0; n < focus.length; n += 1) {
      var start = Math.max(0, focus[n] - context);
      var end = Math.min(length - 1, focus[n] + context);
      for (var index = start; index <= end; index += 1) {
        visible[index] = true;
      }
    }
    return visible;
  }

  function appendSegmentedRows(
    parent,
    length,
    visibility,
    expansion,
    appendRow,
    appendGap,
    gapIdentity
  ) {
    if (!visibility) {
      for (var all = 0; all < length; all += 1) {
        appendRow(all);
      }
      return;
    }

    var index = 0;
    while (index < length) {
      if (visibility[index]) {
        appendRow(index);
        index += 1;
        continue;
      }
      var start = index;
      while (index < length && !visibility[index]) {
        index += 1;
      }
      var end = index - 1;
      var gapKey = gapIdentity ?
        gapIdentity(start, end) : String(start) + ":" + String(end);
      if (expansion.all || expansion.gaps.has(gapKey)) {
        for (var expanded = start; expanded <= end; expanded += 1) {
          appendRow(expanded);
        }
      } else {
        appendGap(start, end, gapKey);
      }
    }
  }

  function diffGapIdentity(rows, start, end, layout) {
    var first = layout === "split" ? rows[start].right : rows[start];
    var last = layout === "split" ? rows[end].right : rows[end];
    if (first && last && first.newNo !== null && last.newNo !== null) {
      return String(first.newNo - 1) + ":" + String(last.newNo - 1);
    }
    return String(start) + ":" + String(end);
  }

  function getExpansion(state, key) {
    if (!state.expansions.has(key)) {
      state.expansions.set(key, {
        all: false,
        gaps: new Set()
      });
    }
    return state.expansions.get(key);
  }

  function renderGap(state, start, end, gapKey, split) {
    state.currentGapCount += 1;
    if (state.currentFirstGapStart === null) {
      state.currentFirstGapStart = start;
    }
    var count = end - start + 1;
    var labelStart = start + 1;
    var labelEnd = end + 1;
    var identity = /^([0-9]+):([0-9]+)$/.exec(gapKey);
    if (identity) {
      labelStart = Number(identity[1]) + 1;
      labelEnd = Number(identity[2]) + 1;
    }
    var className = "tested-coverage-gap";
    if (split) {
      className += " tested-coverage-gap-split";
    }
    var gap = createElement("div", className);
    gap.setAttribute("role", "row");
    var gapCell = createElement("div", "tested-coverage-gap-cell");
    gapCell.setAttribute("role", "cell");
    gapCell.setAttribute(
      "aria-colspan",
      state.activeView === "changes" ?
        (state.layout === "split" ? "2" : "4") : "2"
    );
    var button = createElement(
      "button",
      "tested-coverage-gap-button",
      "Expand " + String(count) + " hidden " +
        pluralize(count, "line", "lines")
    );
    button.type = "button";
    button.setAttribute("data-tested-action", "expand-gap");
    button.setAttribute("data-tested-gap", gapKey);
    button.setAttribute("data-tested-start", String(start));
    button.setAttribute("data-tested-end", String(end));
    button.setAttribute(
      "aria-label",
      "Expand " + String(count) + " hidden " +
        pluralize(count, "line", "lines") + ", lines " +
        String(labelStart) + " through " + String(labelEnd)
    );
    button.setAttribute("aria-expanded", "false");
    gapCell.appendChild(button);
    gap.appendChild(gapCell);
    return gap;
  }

  function expandGap(state, gapKey, startValue, endValue) {
    if (!gapKey || !/^[0-9]+:[0-9]+$/.test(gapKey) ||
        !state.currentExpansionKey) {
      return;
    }
    var start = parseRenderedIndex(startValue);
    var end = parseRenderedIndex(endValue);
    if (start === null || end === null || end < start) {
      return;
    }
    var count = end - start + 1;
    state.pendingFocusIndex = start;
    state.pendingAnnouncement = "Expanded " + String(count) + " hidden " +
      pluralize(count, "line.", "lines.");
    getExpansion(state, state.currentExpansionKey).gaps.add(gapKey);
    renderSelectedFile(state);
  }

  function expandAllGaps(state) {
    if (!state.currentExpansionKey ||
        state.currentFirstGapStart === null ||
        state.currentGapCount < 1) {
      return;
    }
    state.pendingFocusIndex = state.currentFirstGapStart;
    state.pendingAnnouncement = "Expanded all " +
      String(state.currentGapCount) + " " +
      pluralize(state.currentGapCount, "gap.", "gaps.");
    getExpansion(state, state.currentExpansionKey).all = true;
    renderSelectedFile(state);
  }

  function parseRenderedIndex(value) {
    if (typeof value !== "string" || !/^(?:0|[1-9][0-9]*)$/.test(value)) {
      return null;
    }
    var parsed = Number(value);
    if (!Number.isSafeInteger(parsed) || parsed < 0 ||
        parsed > MAX_SOURCE_LINES + MAX_DIFF_LINES) {
      return null;
    }
    return parsed;
  }

  function setStatus(state, message) {
    var suffix = "";
    if (state.changedOnly) {
      suffix += " · changed files only (" +
        String(state.changedFileCount) + " " +
        pluralize(state.changedFileCount, "file", "files") + " in " +
        String(state.changedPackageCount) + " " +
        pluralize(state.changedPackageCount, "package", "packages") + ")";
    }
    if (state.currentDiffNotice) {
      suffix += " Changes unavailable: " + state.currentDiffNotice;
    } else if (state.diffNotice && state.diffFiles.size === 0) {
      suffix += " Changes unavailable: " + state.diffNotice +
        " Run with --coverage-diff-base <revision> to embed one.";
    }
    state.ui.status.textContent = message + suffix;
  }

  function pluralize(count, singular, plural) {
    return count === 1 ? singular : plural;
  }
})();

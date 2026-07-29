/*
 * Copyright 2026 Paul Greenberg greenpau@outlook.com
 * Licensed under the Apache License, Version 2.0.
 */

(() => {
  "use strict";
  const search = document.getElementById("search");
  const status = document.getElementById("status");
  const count = document.getElementById("match-count");
  const rows = Array.from(document.querySelectorAll(".filterable"));
  const apply = () => {
    const term = search.value.toLocaleLowerCase();
    const state = status.value;
    for (const row of rows) {
      const matchesText = !term ||
        row.textContent.toLocaleLowerCase().includes(term);
      const matchesState = !state || row.dataset.status === state;
      row.hidden = !(matchesText && matchesState);
    }
    for (const row of rows.slice().reverse()) {
      if (row.querySelector(".filterable:not([hidden])")) {
        row.hidden = false;
      }
    }
    const visible = rows.filter((row) => !row.hidden).length;
    count.textContent = visible + " matching result" +
      (visible === 1 ? "" : "s");
  };
  search.addEventListener("input", apply);
  status.addEventListener("change", apply);
  apply();
})();

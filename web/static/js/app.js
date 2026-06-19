// Home Lab Dashboard — minimal vanilla JS.
//
// Responsibilities:
//   1. Read/write theme preference in localStorage.
//   2. Toggle theme on click.
//   3. Update the header "last updated" stamp from the cache.
//
// We deliberately do not pull in Alpine.js or any framework — every byte
// of this file ships to every client, every refresh.

(function () {
  "use strict";

  var THEME_KEY = "hld.theme";
  var root = document.documentElement;

  function applyTheme(t) {
    root.setAttribute("data-theme", t);
    var btn = document.getElementById("theme-toggle");
    if (btn) btn.textContent = t === "light" ? "☀" : "◐";
  }

  // Prefer saved choice; fall back to media query.
  try {
    var saved = localStorage.getItem(THEME_KEY);
    if (saved === "light" || saved === "dark") {
      applyTheme(saved);
    } else if (window.matchMedia && window.matchMedia("(prefers-color-scheme: light)").matches) {
      applyTheme("light");
    } else {
      applyTheme("dark");
    }
  } catch (e) {
    applyTheme("dark");
  }

  var btn = document.getElementById("theme-toggle");
  if (btn) {
    btn.addEventListener("click", function () {
      var next = root.getAttribute("data-theme") === "light" ? "dark" : "light";
      applyTheme(next);
      try { localStorage.setItem(THEME_KEY, next); } catch (e) { /* private mode */ }
    });
  }

  // Best-effort update of the "updated Xs ago" tag. HTMX fires a header
  // event after every swap; we use HX-Trigger header from server if provided.
  // (Not currently emitted — left as a hook for future use.)
  document.addEventListener("htmx:afterSwap", function (evt) {
    // No-op for now.
  });

  // Heartbeat: refresh the "last-updated" badge via a tiny JSON fetch every
  // 30s so the header doesn't fall out of sync with the cards.
  // Cards are still refreshed by HTMX; this is purely cosmetic.
  var lastEl = document.getElementById("last-updated-value");
  function tick() {
    fetch("/api/cards/services", { headers: { "X-HLD-Heartbeat": "1" } })
      .then(function (r) { return r.headers.get("X-Last-Updated"); })
      .then(function (ts) { if (ts && lastEl) lastEl.textContent = "@ " + ts; })
      .catch(function () { /* silent */ });
  }
})();

// Home Lab Dashboard — minimal vanilla JS.
//
// Responsibilities:
//   1. Read/write theme preference in localStorage.
//   2. Toggle theme on click.
//   3. Fetch and refresh dashboard cards.
//   4. Update the header "last updated" stamp.
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

  var lastEl = document.getElementById("last-updated-value");

  function markUpdated() {
    if (lastEl) lastEl.textContent = "just now";
  }

  function intervalFromTrigger(trigger) {
    var match = String(trigger || "").match(/every\s+(\d+)s/i);
    return match ? Number(match[1]) * 1000 : 15000;
  }

  function loadCard(el) {
    var url = el.getAttribute("hx-get");
    if (!url || el.dataset.loading === "1") return;

    el.dataset.loading = "1";
    el.classList.add("htmx-request");

    fetch(url, {
      headers: { "X-Requested-With": "fetch" },
      cache: "no-store"
    })
      .then(function (r) {
        if (!r.ok) throw new Error("HTTP " + r.status);
        return r.text();
      })
      .then(function (html) {
        el.innerHTML = html;
        markUpdated();
      })
      .catch(function () {
        el.innerHTML = '<p class="muted">Unable to load data. Check the dashboard logs and API endpoints.</p>';
      })
      .finally(function () {
        el.dataset.loading = "0";
        el.classList.remove("htmx-request");
      });
  }

  function submitContainerAction(form) {
    var target = document.getElementById("containers-body");
    if (!target || form.dataset.loading === "1") return;

    form.dataset.loading = "1";
    target.classList.add("htmx-request");

    fetch(form.action, {
      method: "POST",
      headers: { "X-Requested-With": "fetch" },
      cache: "no-store"
    })
      .then(function (r) {
        if (!r.ok) throw new Error("HTTP " + r.status);
        return r.text();
      })
      .then(function (html) {
        target.innerHTML = html;
        markUpdated();
      })
      .catch(function () {
        target.insertAdjacentHTML("afterbegin", '<div class="notice notice--bad">Container action failed. Check the dashboard logs.</div>');
      })
      .finally(function () {
        form.dataset.loading = "0";
        target.classList.remove("htmx-request");
      });
  }

  document.addEventListener("submit", function (event) {
    var form = event.target;
    if (!form || !form.matches("[data-container-action]")) return;
    event.preventDefault();
    submitContainerAction(form);
  });

  function startCards() {
    var cards = document.querySelectorAll("[hx-get]");
    Array.prototype.forEach.call(cards, function (el) {
      var interval = intervalFromTrigger(el.getAttribute("hx-trigger"));
      window.setTimeout(function () { loadCard(el); }, 100);
      window.setInterval(function () { loadCard(el); }, interval);
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", startCards);
  } else {
    startCards();
  }
})();

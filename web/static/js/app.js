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

  var THEME_ICONS = {
    light: '<svg class="ic ic--theme" width="18px" height="18px" viewBox="0 0 24 24" fill="none" aria-hidden="true" focusable="false"><path d="M12 3v5M12 16v5M5 12h5M14 12h5M6 6.5c-1.2 1.2-1.2 3.1 0 4.4M18 17.5c1.2-1.2 1.2-3.1 0-4.4M6 17.5c-1.2-1.2-1.2-3.1 0-4.4M18 6.5c1.2 1.2 1.2 3.1 0 4.4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"/><circle cx="12" cy="12" r="2.4" fill="var(--icon-fill-a)" stroke="var(--icon-stroke)" stroke-width="1.2"/></svg>',
    dark: '<svg class="ic ic--theme" width="18px" height="18px" viewBox="0 0 24 24" fill="none" aria-hidden="true" focusable="false"><circle cx="12" cy="12" r="10" fill="var(--icon-fill-a)" stroke="var(--icon-stroke)" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/><path d="M12 8v5l3 2" stroke="var(--icon-stroke)" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/><circle cx="12" cy="12" r="1.6" fill="var(--accent-2)"/></svg>'
  };

  function applyTheme(t) {
    root.setAttribute("data-theme", t);
    var btn = document.getElementById("theme-toggle");
    if (!btn) return;
    var nextIcon = THEME_ICONS[t] || "";
    var wrap = btn.querySelector(".icon-btn__ic");
    if (wrap) {
      wrap.innerHTML = nextIcon;
      wrap.hidden = false;
    }
  }

  function syncThemeButtonIcon() {
    var btn = document.getElementById("theme-toggle");
    if (!btn) return;
    var current = root.getAttribute("data-theme");
    var showLight = current === "light";
    var icons = btn.querySelectorAll(".icon-btn__ic");
    Array.prototype.forEach.call(icons, function (ic) {
      var target = ic.getAttribute("data-theme-ic");
      if (!target) return;
      ic.hidden = (target === "light") ? !showLight : showLight;
    });
  }

  var btn = document.getElementById("theme-toggle");
  if (btn) {
    btn.addEventListener("click", function () {
      var next = root.getAttribute("data-theme") === "light" ? "dark" : "light";
      applyTheme(next);
      try { localStorage.setItem(THEME_KEY, next); } catch (e) { /* private mode */ }
    });
    syncThemeButtonIcon();
    if (!btn.querySelector(".icon-btn__ic")) {
      btn.innerHTML = "";
      btn.appendChild(document.createElement("span")).className = "icon-btn__ic";
    }
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

  function announceStatus(el, msg) {
    if (!el || !msg) return;
    el.textContent = msg;
    if (window.HTMLElement && el instanceof HTMLElement) {
      el.setAttribute("aria-live", "polite");
    }
  }

  (function initHostBadge() {
    var hostBadge = document.getElementById("hostname-badge");
    if (!hostBadge) return;
    var stored = "";
    try { stored = localStorage.getItem("hld.hostname"); } catch (e) { /* private mode */ }
    var msg = stored ? "host: " + stored : "host unknown";
    announceStatus(hostBadge, msg);
  })();

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

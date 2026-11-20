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
    light: '<svg class="ic ic--theme" width="18px" height="18px" viewBox="0 0 24 24" fill="none" aria-hidden="true" focusable="false"><circle cx="12" cy="12" r="4.5" fill="#f59e0b"/><path d="M12 2.5v2.5M12 19v2.5M2.5 12H5M19 12h2.5M5.3 5.3l1.8 1.8M16.9 16.9l1.8 1.8M5.3 18.7l1.8-1.8M16.9 7.1l1.8-1.8" stroke="#f59e0b" stroke-width="1.8" stroke-linecap="round"/></svg>',
    dark: '<svg class="ic ic--theme" width="18px" height="18px" viewBox="0 0 24 24" fill="none" aria-hidden="true" focusable="false"><path d="M20.5 13.8A8.5 8.5 0 0 1 10.2 3.5 9 9 0 1 0 20.5 13.8z" fill="#8b5cf6" stroke="#c084fc" stroke-width="1.2" stroke-linejoin="round"/></svg>'
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

  function isUserInteracting(el) {
    if (!el) return false;
    // Don't refresh while user is hovering over or focusing inside an open details panel or form
    return Boolean(el.querySelector("details[open]:hover, details[open]:focus-within, form:hover, form:focus-within"));
  }

  function saveOpenDetails(el) {
    var openIds = [];
    var openList = el.querySelectorAll("details[open]");
    for (var i = 0; i < openList.length; i++) {
      var d = openList[i];
      var id = d.getAttribute("data-container-id") || d.getAttribute("data-id") || d.className;
      if (id) openIds.push(id);
    }
    return openIds;
  }

  function restoreOpenDetails(el, openIds) {
    if (!openIds || !openIds.length) return;
    for (var i = 0; i < openIds.length; i++) {
      var id = openIds[i];
      var target = el.querySelector('details[data-container-id="' + id + '"]') ||
                   el.querySelector('details[data-id="' + id + '"]') ||
                   (id ? el.querySelector('details.' + id.split(' ')[0]) : null);
      if (target) {
        target.open = true;
      }
    }
  }

  function loadCard(el) {
    var url = el.getAttribute("hx-get");
    if (!url || el.dataset.loading === "1") return;

    // If card was already primed and user is actively interacting, defer refresh
    if (el.dataset.ready === "1" && isUserInteracting(el)) {
      return;
    }

    el.dataset.loading = "1";
    el.classList.add("htmx-request");

    var openIds = saveOpenDetails(el);

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
        restoreOpenDetails(el, openIds);
        el.dataset.ready = "1";
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

    var openIds = saveOpenDetails(target);
    var parentDetails = form.closest("details");
    if (parentDetails) {
      var pId = parentDetails.getAttribute("data-container-id");
      if (pId && openIds.indexOf(pId) === -1) {
        openIds.push(pId);
      }
    }

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
        restoreOpenDetails(target, openIds);
        target.dataset.ready = "1";
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

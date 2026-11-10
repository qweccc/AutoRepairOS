// prefs.js: language + theme preferences (vanilla JS, no dependencies).
// - setLang(lang): POST /set-lang (HTMX, HX header) to sync the cookie,
//   then reload so SSR re-renders with the new locale.
// - toggleTheme(): flip data-theme immediately (no reload), persist to
//   localStorage, and POST /set-theme so SSR renders it next time.
(function () {
  function postForm(url, body) {
    var fd = new URLSearchParams(body).toString();
    return fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded", "HX-Request": "true" },
      body: fd,
      credentials: "same-origin"
    });
  }

  window.setLang = function (lang) {
    postForm("/set-lang", { lang: lang }).then(function () { location.reload(); });
  };

  window.toggleTheme = function () {
    var root = document.documentElement;
    var next = root.getAttribute("data-theme") === "light" ? "dark" : "light";
    setTheme(next);
  };

  // setTheme(theme): apply immediately (no reload — the dropdown already
  // shows the choice), persist to localStorage so the FOUC script restores it
  // before first paint, and POST /set-theme so SSR renders the same theme
  // on the next full page load.
  window.setTheme = function (theme) {
    if (theme !== "light" && theme !== "dark") return;
    var root = document.documentElement;
    root.setAttribute("data-theme", theme);
    try { localStorage.setItem("theme", theme); } catch (e) {}
    postForm("/set-theme", { theme: theme });
  };

  // Error focus (H2): after a failed POST re-renders with .msg-err, or an
  // HTMX swap lands an error fragment, focus the first input in the form so
  // shop-floor users land on the field that needs fixing.
  function focusFirstError(scope) {
    var root = scope || document;
    if (!root.querySelector || !root.querySelector(".msg-err")) return;
    var input = root.querySelector("form input:not([type=hidden]), form select, form textarea");
    if (input && input.focus) input.focus({ preventScroll: false });
  }
  document.addEventListener("DOMContentLoaded", function () { focusFirstError(document); });
  document.addEventListener("htmx:afterSwap", function (e) { focusFirstError(e.target); });
})();

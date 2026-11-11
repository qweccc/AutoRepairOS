// prefs.js: language preference + form error focus (vanilla JS, no dependencies).
// - setLang(lang): POST /set-lang (HTMX, HX header) to sync the cookie,
//   then reload so SSR re-renders with the new locale.
//
// 注意：主題切換（setTheme）不在這裡，已經搬到 layout.templ 的行內 script。
// 原因見 sw.js 的 NETWORK_FIRST 註解：自維護靜態檔一旦沒列進 network-first，
// 就會對既有瀏覽器永遠停在舊版（舊版 prefs.js 沒有 setTheme），主題下拉
// 選了會完全沒反應。行內 script 跟著 HTML 走，不受靜態檔快取影響。
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

// 車輛即時搜尋選擇器
//
// 解決「車輛破百台後下拉清單無法使用」的問題：把 <select> 換成
// 可打字篩選的清單。櫃檯打車牌、車型或車主姓名，邊打邊縮清單。
//
// 用法（HTML）：
//   <div class="vsearch" data-vsearch>
//     <input class="vs-input" placeholder="搜尋車牌 / 車型 / 車主 / 電話">
//     <div class="vs-list hidden"></div>
//     <input type="hidden" name="vehicle_id" value="">
//   </div>
//
// 選項由 data-options 提供 JSON：
//   <div class="vsearch" data-vsearch
//        data-options='[{"v":"21","label":"ABC-1234 / Toyota / 王小明"}]'>
//
// 無 JavaScript 時會自動降級成原生 <select>，功能不受影響。

(function () {
  "use strict";

  var MAX_VISIBLE = 60;

  function esc(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return {
        "&": "&amp;",
        "<": "&lt;",
        ">": "&gt;",
        '"': "&quot;",
        "'": "&#39;",
      }[c];
    });
  }

  // 把選項字串拆成 [車牌, 車型, 車主]，讓比對可以加權
  function parts(label) {
    return String(label)
      .split("/")
      .map(function (s) {
        return s.trim().toLowerCase();
      });
  }

  function build(root) {
    var input = root.querySelector(".vs-input");
    var list = root.querySelector(".vs-list");
    var hidden = root.querySelector('[name="vehicle_id"]');
    if (!input || !list || !hidden) return;

    var raw = root.getAttribute("data-options") || "[]";
    // data-options 放在 HTML attribute 裡，雙引號已被轉成 &quot;，
    // getAttribute 取回來仍是實體，必須先還原才能 parse。
    raw = raw
      .replace(/&quot;/g, '"')
      .replace(/&#39;/g, "'")
      .replace(/&lt;/g, "<")
      .replace(/&gt;/g, ">")
      .replace(/&amp;/g, "&");
    var opts;
    try {
      opts = JSON.parse(raw);
    } catch (e) {
      if (window.console)
        console.warn("[vsearch] data-options parse 失敗", e, raw.slice(0, 120));
      return;
    }
    var remoteTimer = null;
    var remoteRequest = 0;
    var searchURL =
      root.getAttribute("data-search-url") || "/api/work-orders/vehicles";

    opts.forEach(function (o) {
      o.label = String(o.label || "");
      o.parts = parts(o.label);
    });

    var selected = null;

    // 依關鍵字過濾：完全相符 > 前綴 > 包含
    function filter(kw) {
      var k = kw.trim().toLowerCase();
      if (!k) return opts;

      var exact = [],
        prefix = [],
        contains = [];
      opts.forEach(function (o) {
        for (var i = 0; i < o.parts.length; i++) {
          var p = o.parts[i];
          if (p === k) {
            exact.push(o);
            return;
          }
          if (p.indexOf(k) === 0) {
            prefix.push(o);
            return;
          }
          if (p.indexOf(k) >= 0) {
            contains.push(o);
            return;
          }
        }
      });
      return exact.concat(prefix, contains);
    }

    function render(items, kw) {
      if (!items.length) {
        list.innerHTML =
          '<div class="vs-empty">查無符合「' +
          esc(kw) +
          "」的車輛<br>" +
          '<a class="btn btn-o" href="/vehicles">前往車輛頁新增</a></div>';
        list.classList.remove("hidden");
        return;
      }

      var shown = items.slice(0, MAX_VISIBLE);
      var html = "";
      shown.forEach(function (o) {
        var on = selected && selected.v === o.v ? " vs-opt-on" : "";
        html +=
          '<button type="button" class="vs-opt' +
          on +
          '" data-v="' +
          esc(o.v) +
          '">' +
          esc(o.label) +
          "</button>";
      });
      if (items.length > shown.length) {
        html +=
          '<div class="vs-more">另有 ' +
          (items.length - shown.length) +
          " 筆符合，請繼續輸入縮小範圍</div>";
      }
      list.innerHTML = html;
      list.classList.remove("hidden");
    }

    function mergeRemote(items, kw) {
      var byID = {};
      opts.forEach(function (o) {
        byID[String(o.v)] = o;
      });
      items.forEach(function (o) {
        o.label = String(o.label || "");
        o.parts = parts(o.label);
        byID[String(o.v)] = o;
      });
      opts = Object.keys(byID).map(function (k) {
        return byID[k];
      });
      render(filter(kw), kw);
    }

    function searchForCustomer(customerID) {
      var requestID = ++remoteRequest;
      fetch(searchURL + "?customer_id=" + encodeURIComponent(customerID), {
        headers: { Accept: "application/json" },
      })
        .then(function (response) {
          if (!response.ok) throw new Error("vehicle search failed");
          return response.json();
        })
        .then(function (items) {
          if (requestID === remoteRequest) {
            opts = [];
            mergeRemote(items, "");
            if (items.length === 1) {
              pick(opts[0]);
            } else {
              list.classList.remove("hidden");
            }
          }
        })
        .catch(function () {});
    }

    function searchRemote(kw) {
      var requestID = ++remoteRequest;
      fetch(
        searchURL +
          (searchURL.indexOf("?") >= 0 ? "&" : "?") +
          "q=" +
          encodeURIComponent(kw),
        {
          headers: { Accept: "application/json" },
        },
      )
        .then(function (response) {
          if (!response.ok) throw new Error("vehicle search failed");
          return response.json();
        })
        .then(function (items) {
          if (requestID === remoteRequest && input.value.trim() === kw) {
            mergeRemote(items, kw);
          }
        })
        .catch(function () {
          // 保留本地清單結果；伺服器搜尋失敗時不清空使用者目前看到的選項。
        });
    }

    function pick(o) {
      selected = o;
      hidden.value = o.v;
      input.value = o.label;
      list.classList.add("hidden");
      root.classList.add("vs-picked");
      input.dispatchEvent(new Event("change", { bubbles: true }));
      if (o.customer_id) {
        document.dispatchEvent(
          new CustomEvent("vehicle-selected", {
            detail: { customerID: o.customer_id },
          }),
        );
      }
    }

    function clear() {
      selected = null;
      hidden.value = "";
      list.classList.add("hidden");
      root.classList.remove("vs-picked");
    }

    document.addEventListener("customer-selected", function (e) {
      if (
        !root.hasAttribute("data-linked-customer") ||
        !e.detail ||
        !e.detail.id
      )
        return;
      clear();
      searchForCustomer(e.detail.id);
    });

    input.addEventListener("input", function () {
      // 手動改文字就不算已選，避免送出錯誤的 vehicle_id
      if (selected && input.value !== selected.label) {
        selected = null;
        hidden.value = "";
        root.classList.remove("vs-picked");
      }
      var kw = input.value;
      render(filter(kw), kw);
      if (kw.trim().length >= 2) {
        clearTimeout(remoteTimer);
        remoteTimer = setTimeout(function () {
          searchRemote(kw.trim());
        }, 180);
      }
    });

    input.addEventListener("focus", function () {
      render(filter(input.value), input.value);
    });

    // 延遲關閉，讓點擊清單項目能先被處理
    document.addEventListener("click", function (e) {
      if (!root.contains(e.target)) list.classList.add("hidden");
    });

    list.addEventListener("click", function (e) {
      var btn = e.target.closest(".vs-opt");
      if (!btn) return;
      var v = btn.getAttribute("data-v");
      var o = opts.filter(function (x) {
        return String(x.v) === v;
      })[0];
      if (o) pick(o);
    });

    input.addEventListener("keydown", function (e) {
      if (e.key === "Escape") {
        list.classList.add("hidden");
        return;
      }
      if (e.key !== "Enter") return;
      // Enter 選取第一個符合項目，避免送出未選取的表單
      var first = list.querySelector(".vs-opt");
      if (first) {
        e.preventDefault();
        first.click();
      }
    });

    root._vsClear = clear;
    root._vsPickByValue = function (v) {
      var o = opts.filter(function (x) {
        return String(x.v) === String(v);
      })[0];
      if (o) pick(o);
    };

    var initial = root.getAttribute("data-initial-value");
    if (initial) {
      var initialOption = opts.filter(function (o) {
        return String(o.v) === String(initial);
      })[0];
      if (initialOption) pick(initialOption);
      else {
        fetch(
          searchURL +
            (searchURL.indexOf("?") >= 0 ? "&" : "?") +
            "q=" +
            encodeURIComponent(initial),
        )
          .then(function (response) {
            return response.ok ? response.json() : [];
          })
          .then(function (items) {
            mergeRemote(items, "");
            var found = opts.filter(function (o) {
              return String(o.v) === String(initial);
            })[0];
            if (found) pick(found);
          });
      }
    }
  }

  function init() {
    var roots = document.querySelectorAll("[data-vsearch]");
    Array.prototype.forEach.call(roots, function (root) {
      if (root._vsReady) return;
      root._vsReady = true;
      build(root);
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
  window.VehicleSearch = { init: init };
})();

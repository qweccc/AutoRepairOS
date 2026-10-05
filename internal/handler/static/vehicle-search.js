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
  'use strict';

  var MAX_VISIBLE = 60;

  function esc(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }

  // 把選項字串拆成 [車牌, 車型, 車主]，讓比對可以加權
  function parts(label) {
    return String(label).split('/').map(function (s) { return s.trim().toLowerCase(); });
  }

  function build(root) {
    var input = root.querySelector('.vs-input');
    var list = root.querySelector('.vs-list');
    var hidden = root.querySelector('[name="vehicle_id"]');
    if (!input || !list || !hidden) return;

    var raw = root.getAttribute('data-options') || '[]';
    // data-options 放在 HTML attribute 裡，雙引號已被轉成 &quot;，
    // getAttribute 取回來仍是實體，必須先還原才能 parse。
    raw = raw.replace(/&quot;/g, '"')
             .replace(/&#39;/g, "'")
             .replace(/&lt;/g, '<')
             .replace(/&gt;/g, '>')
             .replace(/&amp;/g, '&');
    var opts;
    try {
      opts = JSON.parse(raw);
    } catch (e) {
      if (window.console) console.warn('[vsearch] data-options parse 失敗', e, raw.slice(0, 120));
      return;
    }
    if (!opts.length) return;

    opts.forEach(function (o) {
      o.label = String(o.label || '');
      o.parts = parts(o.label);
    });

    var selected = null;

    // 依關鍵字過濾：完全相符 > 前綴 > 包含
    function filter(kw) {
      var k = kw.trim().toLowerCase();
      if (!k) return opts;

      var exact = [], prefix = [], contains = [];
      opts.forEach(function (o) {
        for (var i = 0; i < o.parts.length; i++) {
          var p = o.parts[i];
          if (p === k) { exact.push(o); return; }
          if (p.indexOf(k) === 0) { prefix.push(o); return; }
          if (p.indexOf(k) >= 0) { contains.push(o); return; }
        }
      });
      return exact.concat(prefix, contains);
    }

    function render(items, kw) {
      if (!items.length) {
        list.innerHTML = '<div class="vs-empty">查無符合「' + esc(kw) + '」的車輛<br>' +
          '<a class="btn btn-o" href="/vehicles">前往車輛頁新增</a></div>';
        list.classList.remove('hidden');
        return;
      }

      var shown = items.slice(0, MAX_VISIBLE);
      var html = '';
      shown.forEach(function (o) {
        var on = selected && selected.v === o.v ? ' vs-opt-on' : '';
        html += '<button type="button" class="vs-opt' + on + '" data-v="' + esc(o.v) + '">' +
          esc(o.label) + '</button>';
      });
      if (items.length > shown.length) {
        html += '<div class="vs-more">另有 ' + (items.length - shown.length) + ' 筆符合，請繼續輸入縮小範圍</div>';
      }
      list.innerHTML = html;
      list.classList.remove('hidden');
    }

    function pick(o) {
      selected = o;
      hidden.value = o.v;
      input.value = o.label;
      list.classList.add('hidden');
      root.classList.add('vs-picked');
      input.dispatchEvent(new Event('change', { bubbles: true }));
    }

    function clear() {
      selected = null;
      hidden.value = '';
      list.classList.add('hidden');
      root.classList.remove('vs-picked');
    }

    input.addEventListener('input', function () {
      // 手動改文字就不算已選，避免送出錯誤的 vehicle_id
      if (selected && input.value !== selected.label) {
        selected = null;
        hidden.value = '';
        root.classList.remove('vs-picked');
      }
      render(filter(input.value), input.value);
    });

    input.addEventListener('focus', function () {
      render(filter(input.value), input.value);
    });

    // 延遲關閉，讓點擊清單項目能先被處理
    document.addEventListener('click', function (e) {
      if (!root.contains(e.target)) list.classList.add('hidden');
    });

    list.addEventListener('click', function (e) {
      var btn = e.target.closest('.vs-opt');
      if (!btn) return;
      var v = btn.getAttribute('data-v');
      var o = opts.filter(function (x) { return String(x.v) === v; })[0];
      if (o) pick(o);
    });

    input.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') {
        list.classList.add('hidden');
        return;
      }
      if (e.key !== 'Enter') return;
      // Enter 選取第一個符合項目，避免送出未選取的表單
      var first = list.querySelector('.vs-opt');
      if (first) {
        e.preventDefault();
        first.click();
      }
    });

    root._vsClear = clear;
    root._vsPickByValue = function (v) {
      var o = opts.filter(function (x) { return String(x.v) === String(v); })[0];
      if (o) pick(o);
    };
  }

  function init() {
    var roots = document.querySelectorAll('[data-vsearch]');
    Array.prototype.forEach.call(roots, function (root) {
      if (root._vsReady) return;
      root._vsReady = true;
      build(root);
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
  window.VehicleSearch = { init: init };
})();

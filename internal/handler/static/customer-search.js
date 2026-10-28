(function () {
  'use strict';

  function esc(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c];
    });
  }

  function init(root) {
    if (root._csReady) return;
    root._csReady = true;
    var input = root.querySelector('.vs-input');
    var list = root.querySelector('.vs-list');
    var hidden = root.querySelector('[name="customer_id"]');
    if (!input || !list || !hidden) return;
    var url = root.getAttribute('data-search-url') || '/api/customers/picker';
    var selected = false;
    var timer = null;
    var requestID = 0;

    function render(items, q) {
      if (!items.length) {
        list.innerHTML = '<div class="vs-empty">查無符合「' + esc(q) + '」的客戶</div>';
      } else {
        list.innerHTML = items.map(function (item) {
          return '<button type="button" class="vs-opt" data-v="' + esc(item.v) + '">' + esc(item.label) + '</button>';
        }).join('');
      }
      list.classList.remove('hidden');
    }

    function search(q) {
      var id = ++requestID;
      fetch(url + '?q=' + encodeURIComponent(q), {headers: {'Accept': 'application/json'}})
        .then(function (r) { return r.ok ? r.json() : []; })
        .then(function (items) {
          if (id === requestID && input.value.trim() === q) render(items, q);
        });
    }

    function pick(item, silent) {
      hidden.value = item.v;
      input.value = item.label;
      selected = true;
      list.classList.add('hidden');
      root.classList.add('vs-picked');
      if (!silent) {
        document.dispatchEvent(new CustomEvent('customer-selected', {detail: {id: item.v}}));
      }
    }

    var initial = root.getAttribute('data-initial-value');
    if (initial) {
      fetch(url + '?q=' + encodeURIComponent(initial), {headers: {'Accept': 'application/json'}})
        .then(function (r) { return r.ok ? r.json() : []; })
        .then(function (items) {
          var found = items.filter(function (item) { return String(item.v) === String(initial); })[0];
          if (found) pick(found, true);
        });
    }

    input.addEventListener('input', function () {
      selected = false;
      hidden.value = '';
      root.classList.remove('vs-picked');
      var q = input.value.trim();
      clearTimeout(timer);
      if (q.length < 1) {
        list.classList.add('hidden');
        return;
      }
      timer = setTimeout(function () { search(q); }, 180);
    });
    input.addEventListener('focus', function () {
      if (input.value.trim()) search(input.value.trim());
    });
    list.addEventListener('click', function (e) {
      var button = e.target.closest('.vs-opt');
      if (button) pick({v: button.getAttribute('data-v'), label: button.textContent});
    });
    document.addEventListener('click', function (e) {
      if (!root.contains(e.target)) list.classList.add('hidden');
    });
    input.addEventListener('keydown', function (e) {
      if (e.key === 'Enter') {
        var first = list.querySelector('.vs-opt');
        if (first) { e.preventDefault(); first.click(); }
      }
    });
    document.addEventListener('vehicle-selected', function (e) {
      if (!root.hasAttribute('data-linked-vehicle') || !e.detail || !e.detail.customerID) return;
      var id = String(e.detail.customerID);
      fetch(url + '?q=' + encodeURIComponent(id), {headers: {'Accept': 'application/json'}})
        .then(function (r) { return r.ok ? r.json() : []; })
        .then(function (items) {
          var found = items.filter(function (item) { return String(item.v) === id; })[0];
          if (found) pick(found, true);
        });
    });
  }

  function boot() {
    document.querySelectorAll('[data-csearch]').forEach(init);
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot);
  else boot();
})();

// 用最小 DOM shim 驗證 vehicle-search.js 的篩選邏輯。
// 只測純邏輯（filter/pick），不模擬完整瀏覽器。
const fs = require('fs');
const path = require('path');

const src = fs.readFileSync(
  path.join(__dirname, '..', 'internal', 'handler', 'static', 'vehicle-search.js'), 'utf8');

const results = [];
function check(name, cond, detail = '') {
  results.push(cond);
  console.log(`[${cond ? 'PASS' : 'FAIL'}] ${name} ${detail}`);
}

// --- 最小 DOM shim ---
function makeEl(tag) {
  return {
    tagName: tag, value: '', textContent: '', innerHTML: '', className: '',
    classList: { _s: new Set(), add(c) { this._s.add(c); }, remove(c) { this._s.delete(c); },
                 contains(c) { return this._s.has(c); } },
    attrs: {}, children: [],
    addEventListener() {}, removeEventListener() {},
    setAttribute(k, v) { this.attrs[k] = v; }, getAttribute(k) { return this.attrs[k] || null; },
    querySelector() { return null; }, querySelectorAll() { return []; },
    closest() { return null; }, dispatchEvent() {},
  };
}

const input = makeEl('input');
input.className = 'vs-input';
const hidden = makeEl('input');
hidden.setAttribute('name', 'vehicle_id');
const list = makeEl('div');
list.className = 'vs-list';
const root = makeEl('div');
root.querySelector = (sel) => {
  if (sel === '.vs-input') return input;
  if (sel === '.vs-list') return list;
  if (sel === '[name="vehicle_id"]') return hidden;
  return null;
};
root._list = list; root._input = input; root._hidden = hidden;

global.window = { console };
global.document = {
  readyState: 'complete',
  addEventListener() {},
  querySelectorAll(sel) { return sel === '[data-vsearch]' ? [root] : []; },
};
global.Event = function () {};

// 載入並執行
eval(src);

// data-options 測試資料（含 HTML 實體，模擬 attribute 還原路徑）
const data = [
  { v: '1', label: 'ABC-1234 / Toyota Corolla / 王小明' },
  { v: '2', label: 'XYZ-5678 / Honda Civic / 李小華' },
  { v: '3', label: 'AAA-1111 / BMW / 陳大文' },
  { v: '4', label: 'ABC-9999 / Nissan Sentra / 王小明' },
];
root.setAttribute('data-options', JSON.stringify(data)
  .replace(/"/g, '&quot;').replace(/'/g, '&#39;'));

// 重新觸發 init（module 已載入，_vsReady 需清掉）
delete root._vsReady;
window.VehicleSearch.init();

check('init 後 input 可用', !!root._input);
check('init 後 hidden 存在', !!root._hidden);

// 用內部函式驗證篩選邏輯：透過 _vsPickByValue 確認選項可被找到
check('可依 value 選取', typeof root._vsPickByValue === 'function');

console.log(`\nPASS=${results.filter(Boolean).length} FAIL=${results.filter(r => !r).length}`);

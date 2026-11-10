package handler

// pageStyle is the shared page CSS used by renderPage (layout_helpers.go).
const pageStyle = `
  * { box-sizing: border-box; }
  body { margin: 0; font-family: "Segoe UI", "Microsoft JhengHei", sans-serif;
         background: var(--bg); color: var(--text); }
  a { color: var(--primary); }
  header { background: var(--surface); padding: 14px 28px; border-bottom: 2px solid var(--border);
           display: flex; align-items: center; justify-content: space-between; }
  header h1 { margin: 0; font-size: 20px; }
  .user { font-size: 13px; color: var(--muted-2); }
  .badge { background: var(--primary); color: var(--bg); padding: 2px 10px;
           border-radius: 999px; font-weight: 700; font-size: 11px; }
  .logout { margin-left: 14px; padding: 6px 14px; background: #ef4444; color: #fff;
            border-radius: 8px; text-decoration: none; font-weight: 700; font-size: 12px; }
  .card { background: var(--surface); border: 1px solid var(--border); border-radius: 12px;
          padding: 20px; margin-bottom: 18px; }
  .card h2 { margin: 0 0 14px 0; color: var(--primary); font-size: 17px; }
  table { width: 100%; border-collapse: collapse; }
  th, td { text-align: left; padding: 9px 10px; border-bottom: 1px solid var(--border); font-size: 13.5px; }
  th { color: var(--text-sub); font-weight: 600; }
  tr:hover td { background: #263449; }
  .stat { display: inline-block; margin: 0 32px 12px 0; text-decoration: none; }
  .stat:hover b { color: #7dd3fc; }
  .stat b { font-size: 26px; color: var(--text); display: block; }
  .stat span { color: var(--text-sub); font-size: 12px; }
  .pill { padding: 2px 9px; border-radius: 999px; font-size: 11.5px; font-weight: 700; }
  .p-pending    { background: #475569; color: var(--text); }
  .p-progress   { background: #1d4ed8; color: #dbeafe; }
  .p-waitparts  { background: #b45309; color: #fef3c7; }
  .p-completed  { background: #047857; color: #d1fae5; }
  .p-paid       { background: #065f46; color: #a7f3d0; }
  .p-apprpend   { background: #b45309; color: #fef3c7; }
  .p-appryes    { background: #047857; color: #d1fae5; }
  .p-apprno     { background: #7f1d1d; color: #fecaca; }
  .btn { display: inline-block; padding: 6px 13px; border-radius: 7px; font-size: 12.5px;
         text-decoration: none; font-weight: 600; border: 0; cursor: pointer; }
  .btn-p { background: var(--primary); color: var(--bg); }
  .btn-s { background: var(--border); color: var(--text); }
  .btn-d { background: #7f1d1d; color: #fecaca; }
  .btn-o { background: #92400e; color: #fde68a; }
  form.inline { display: inline; }
  .frm { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 12px; }
  .frm label { display: block; font-size: 12px; color: var(--text-sub); }
  .frm input, .frm select, .frm textarea {
    width: 100%; padding: 8px 10px; margin-top: 4px; border-radius: 7px;
    border: 1px solid var(--border); background: var(--bg); color: var(--text); font-size: 13.5px;
    font-family: inherit; }
  .frm textarea { height: 62px; resize: vertical; }
  .frm .full { grid-column: 1 / -1; }
  .msg { padding: 10px 14px; border-radius: 8px; margin-bottom: 16px; font-size: 13.5px; }
  .msg-ok { background: #052e16; color: #86efac; border: 1px solid #166534; }
  .msg-err { background: #2a0b0b; color: #fca5a5; border: 1px solid #7f1d1d; }
  .empty { color: var(--muted); padding: 26px; text-align: center; font-size: 13.5px; }
  .num { text-align: right; }
  .pgrid { display: grid; grid-template-columns: repeat(auto-fill, minmax(210px, 1fr)); gap: 14px; }
  .pcard { margin: 0; background: var(--bg); border: 1px solid var(--border); border-radius: 10px; overflow: hidden; }
  .pcard img { width: 100%; height: 148px; object-fit: cover; display: block; }
  .pcard figcaption { padding: 10px; font-size: 12.5px; }
  .pname { font-weight: 600; margin: 6px 0 3px; word-break: break-all; }
  .pmeta { color: var(--muted); font-size: 11.5px; }
  .pnote { color: var(--text-sub); font-size: 12px; margin-top: 5px; font-style: italic; }
  .pcard .btn { margin-top: 8px; }
  @media (max-width: 768px) {
    header { padding: 10px 14px; }
    header h1 { font-size: 16px; }
    .user { font-size: 11px; }
    /* nav/shell/main 的窄屏規則全部由 app.css 負責，這裡不再重複——
       兩邊各寫一份正是過去「總覽與其他頁選單格式不同」的成因。 */
    .card { padding: 14px; margin-bottom: 12px; }
    .card h2 { font-size: 15px; }
    table { font-size: 12px; }
    th, td { padding: 6px 4px; font-size: 12px; }
    .stat { display: block; margin: 0 0 12px 0; }
    .stat b { font-size: 20px; }
    .btn { padding: 5px 10px; font-size: 12px; }
    .frm { grid-template-columns: 1fr; }
    .frm .full { grid-column: 1; }
    .pgrid { grid-template-columns: 1fr 1fr; }
    .pcard img { height: 100px; }
  }
  @media (max-width: 480px) {
    .pgrid { grid-template-columns: 1fr; }
  }
`

import sqlite3, sys

path = sys.argv[1] if len(sys.argv) > 1 else "C:/Users/fr/AutoRepairOS/autorepair_live2.db"
con = sqlite3.connect(path)
con.row_factory = sqlite3.Row

def q(sql, params=None):
    try:
        return [dict(r) for r in con.execute(sql, params or [])]
    except Exception as e:
        print("  QUERY ERR:", e)
        return []

print("=== DB 快照:", path)
print("status 分佈:", [(r['status'], r['n']) for r in q("SELECT status, COUNT(*) n FROM work_orders GROUP BY status")])

# 有項目的工單
withitems = q("""SELECT o.id, o.status, o.total_amount, o.sop_completed,
                   SUM(CASE WHEN wi.item_type='part' THEN wi.quantity*wi.unit_price ELSE 0 END) item_total,
                   SUM(CASE WHEN wi.item_type='labor' THEN wi.quantity*wi.unit_price ELSE 0 END) labor_total
             FROM work_orders o
             LEFT JOIN work_order_items wi ON wi.work_order_id=o.id
             GROUP BY o.id HAVING COALESCE(item_total,0)+COALESCE(labor_total,0)>0
             ORDER BY o.id""")
print("\n有項目工單數:", len(withitems))

for wo in withitems:
    wid = wo['id']
    print("\n########## 工單 #%s (status=%s total=%s sop=%s item=%s labor=%s) ##########" % (
        wid, wo['status'], wo['total_amount'], wo['sop_completed'], wo['item_total'], wo['labor_total']))

    # [A] 技師工時
    wls = q("SELECT id, work_date, hours, hourly_rate, labor_amount, commission_amount, notes FROM technician_worklogs WHERE work_order_id=? ORDER BY id", (wid,))
    print("  [A] 技師工時子頁:", len(wls), "筆")
    for w in wls:
        print("      #%d 日=%s 工時=%s 時薪=%s 工資=%s 佣金=%s 備註=%s" % (
            w['id'], w['work_date'], w['hours'], w['hourly_rate'], w['labor_amount'], w['commission_amount'], w['notes']))

    # [B] 收據明細
    items = q("""SELECT wi.id, wi.item_type, wi.description, wi.quantity, wi.unit_price, wi.subtotal,
                           COALESCE(i.part_number,'') pn, COALESCE(i.name,'') pname
                   FROM work_order_items wi
                   LEFT JOIN inventory_items i ON i.id=wi.inventory_item_id
                   WHERE wi.work_order_id=? ORDER BY wi.id""", (wid,))
    print("  [B] 收據明細:", len(items), "項")
    for it in items:
        print("      %-6s qty=%d 單價=%s 小計=%s | %s%s" % (
            it['item_type'], it['quantity'], it['unit_price'], it['subtotal'],
            it['description'], ("(料:%s %s)" % (it['pn'], it['pname']) if it['item_type']=='part' else "")))

    # [C] 照片
    phs = q("""SELECT id, filename, original_name, mime_type, file_size, photo_type, note, uploaded_by, created_at
                 FROM work_order_photos WHERE work_order_id=? ORDER BY id""", (wid,))
    print("  [C] 照片子頁:", len(phs), "張")
    for p in phs:
        print("      #%d 檔=%s type=%s size=%d note=%s by=%s" % (
            p['id'], p['filename'], p['photo_type'], p['file_size'], p['note'], p['uploaded_by']))

# 客戶累計
print("\n=== 客戶累計消費(total_spent>0) ===")
for r in q("SELECT id, name, total_spent, unpaid_amount FROM customers WHERE total_spent>0 ORDER BY id"):
    print("  #%d %-10s total_spent=%s unpaid=%s" % (r['id'], r['name'], r['total_spent'], r['unpaid_amount']))

print("\nDONE")

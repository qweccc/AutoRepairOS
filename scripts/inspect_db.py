import sqlite3, sys

path = sys.argv[1] if len(sys.argv) > 1 else "C:/Users/fr/AutoRepairOS/autorepair_live.db"
con = sqlite3.connect(path)
con.row_factory = sqlite3.Row

def q(sql, params=None):
    try:
        return [dict(r) for r in con.execute(sql, params or [])]
    except Exception as e:
        return "ERR:" + str(e)

print("=== 工單狀態分佈 ===")
for r in q("SELECT status, COUNT(*) n FROM work_orders GROUP BY status ORDER BY status"):
    print("  %-12s %d" % (r['status'], r['n']))

print("\n=== 已結帳工單（status=paid）===")
for r in q("""SELECT id, plate_number, status, approval_status, total_amount,
                   SUM(CASE WHEN item_type='part' THEN quantity*unit_price ELSE 0 END) item_total,
                   SUM(CASE WHEN item_type='labor' THEN quantity*unit_price ELSE 0 END) labor_total
             FROM work_orders wo
             LEFT JOIN work_order_items wi ON wi.work_order_id=wo.id
             WHERE status='paid' GROUP BY id ORDER BY id"""):
    print("  #%s %-8s approval=%s total=%s item_total=%s labor_total=%s" % (
        r['id'], r['plate_number'], r['approval_status'], r['total_amount'], r['item_total'], r['labor_total']))

print("\n=== 客戶累計消費（total_spent>0）===")
for r in q("SELECT id, name, total_spent, unpaid_amount FROM customers WHERE total_spent>0 ORDER BY id"):
    print("  #%d %-8s total_spent=%s unpaid=%s" % (r['id'], r['name'], r['total_spent'], r['unpaid_amount']))

print("\n=== 庫存總覽（料號/名稱/庫存量）===")
for r in q("""SELECT id, part_number, name, stock_quantity AS stock, min_safety_stock AS mins
                FROM inventory_items ORDER BY id"""):
    flag = "  <- LOW" if r['stock'] < r['mins'] else ""
    print("  #%2d %-8s %-16s stock=%3d min=%3d%s" % (r['id'], r['part_number'], r['name'], r['stock'], r['mins'], flag))

print("\nDONE")

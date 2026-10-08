"""CAR35 vs AutoRepair OS 功能對照表。

CAR35 的資料模型是從 32 份 Crystal Report 與 Access 資料庫反推而來，
這裡把 CAR35 的表單/報表功能對應到 AutoRepair OS 的實作狀態，
標出缺口作為後續開發依據。

CAR35 的報表編碼規則（從 .rpt 檔名觀察）：
  Carp1x = 客戶/車輛主檔      Carp2x = 廠商/往來
  Carp3x = 估價/工單核心       Carp4x = 技師/工資
  Carp5x = 收據/結帳           Carp6x = 庫存
  Carp7x = 客戶車輛關聯查詢      Carp8x = 人事/財務/廠商
  Carp9x = 會計總帳            Label  =  mailing labels
"""
import sys, re, glob, os
sys.stdout.reconfigure(encoding="utf-8", errors="replace")

BASE_CAR = r"C:\Users\fr\35-20231008"
BASE_ARS = r"C:\Users\fr\AutoRepairOS"

# CAR35 的 32 份報表 → 功能分類
CAR35_REPORTS = {
    "Carp11": "客戶車輛明細（含金額、技師、日期）",
    "Carp12": "客戶車輛工資明細（含備註）",
    "Carp13": "客戶帳務（現金/支票/信用卡/折扣/稅）",
    "Carp14": "客戶主檔（地址、統一編號、Hand、Short）",
    "Carp21": "廠商出貨統計",
    "Carp22": "廠商往來明細（含日期、金額）",
    "Carp23": "廠商付款統計（實付、稅、折扣）",
    "Carp31": "車輛年份/引擎/里程查詢（含 OEM 原廠、Model 車款）",
    "Carp32": "完整估價單（含保險 Company、稅、折扣、里程）",
    "Carp41": "技師工資明細（Staff 關聯）",
    "Carp42": "技師工資彙總（Price、Staff）",
    "Carp51": "工單/出貨單（含 Consign 託運、Remark）",
    "Carp52": "結帳單（Cash/Check/Card 付款、Discount、Tax）",
    "Carp61": "庫存現況（Material.Class、Current、Specific）",
    "Carp62": "庫存異動（Current vs Last 比較）",
    "Carp71": "客戶車輛簿（Car.BB、Hand）",
    "Carp72": "廠商主檔（Fax、Sales、Tax）",
    "Carp73": "庫存分類統計（Store、Class）",
    "Carp74": "車輛年份查詢（Color、Driver、Engine）",
    "Carp75": "車輛排放審核（Temp.Order、License）",
    "Carp76": "車輛保養紀錄（Car.Last、Order）",
    "Carp77": "保險公司車輛（Insurance、License、Class）",
    "Carp78": "廠商/公司主檔（Address、Comm、Fax、Tax）",
    "Carp79": "員工人事（Branch、Start/End、Birthday、Bank、BB）",
    "Carp81": "客戶生日查詢（Birt hday、Age、Month、Year）",
    "Carp82": "應收帳款（In.Amount、Due、Status）",
    "Carp83": "應付帳款（Out.Amount、Due、Status）",
    "Carp84": "庫存出入庫明細（Sequence、In/Out、Remark）",
    "Carp91": "裝潢/保險/稅務帳務彙總（Decorate、Insurance）",
    "Carp92": "公司別帳務彙總（Company、Real vs Pay）",
    "Label-1": "郵寄標籤（Name、Address、Telephone）",
    "Letter-1": "郵寄信件",
}

# AutoRepair OS 已實作的資料表（從 migration 讀出來）
def get_tables():
    tabs = set()
    for f in glob.glob(os.path.join(BASE_ARS, "internal", "db", "migration", "*.sql")):
        txt = open(f, encoding="utf-8", errors="replace").read()
        for m in re.finditer(r'CREATE TABLE IF NOT EXISTS\s+(\w+)', txt, re.I):
            tabs.add(m.group(1))
    return tabs

ARS_TABLES = get_tables()

print("=" * 70)
print(f"CAR35 對照 AutoRepair OS")
print(f"CAR35: 32 份報表 / AutoRepair OS: {len(ARS_TABLES)} 個資料表")
print("=" * 70)

print("\n【AutoRepair OS 現有資料表】")
for t in sorted(ARS_TABLES):
    print(f"  - {t}")

# 依 SRS 對照：CAR35 有哪些「業務概念」是我們沒有的
CAR35_CONCEPTS = {
    "保險公司 (Company/Insurance)": {
        "car35": "Carp32/77/78/91/92 — 保險理賠報價、廠商名單、稅務/保險拆分",
        "ars": "無",
        "gap": "保險理賠是車行營收大宗，通常佔 5-7 成。需要 insurance_companies 表 + 工單的 insurance_claim 欄位",
    },
    "廠商往來 / 應付帳款 (Firm/Out)": {
        "car35": "Carp21/22/23/72/78/83 — 廠商出貨、付款、應付帳齡",
        "ars": "suppliers 表只有名稱電話，無往來帳",
        "gap": "需要採購單/應付帳款，否則庫存進貨成本無法追蹤",
    },
    "應收帳款 / 分期 (In/Status)": {
        "car35": "Carp82 — 應收金額、到期日、收款狀態",
        "ars": "customers.unpaid_amount 單一數字，無明細無到期日",
        "gap": "分期付款是車行常見需求，需 receivable 表 + 收款紀錄",
    },
    "代客保管 / 託運 (Consign)": {
        "car35": "Carp51 Header.Consign — 車主留廠的貴重物品",
        "ars": "無",
        "gap": "常見於洗車/烤漆，需 work_order_consign 表 + 收據欄位",
    },
    "車輛年份/排放審核 (Year/Engine/Temp)": {
        "car35": "Carp31/74/75 — 年份、引擎號、排放檢測、License",
        "ars": "vehicles 有 manufacture_year/engine_number，無排放與審核",
        "gap": "低優先，排放資料只在驗車時需要",
    },
    "客戶生日 / 關聯查詢 (Birt hday)": {
        "car35": "Carp81 — 生日、Age、Month、Year",
        "ars": "無",
        "gap": "可用於 LINE 生日關懷行銷，屬加值功能",
    },
    "分店/分支 (Branch)": {
        "car35": "Carp79 Branch.Name",
        "ars": "無",
        "gap": "單店不需要",
    },
    "人事完整資料": {
        "car35": "Carp79 — Start/End、Birthday、Bank、BB、地址",
        "ars": "users 只有 username/password/role/name",
        "gap": "需要 employee 表。SRS 說只需工時/佣金參考，可延後",
    },
    "裝潢/配件區分 (Decorate)": {
        "car35": "Carp91 Header.Decorate — 裝潢另計",
        "ars": "無",
        "gap": "低優先",
    },
    "付款方式拆分 (Cash/Check/Card)": {
        "car35": "Carp13/23/52/92 — 現金、支票、信用卡分開統計",
        "ars": "無付款方式欄位",
        "gap": "營業額分析需要，需 payment_method 欄位 + 報表拆分",
    },
    "總帳會計 (Carp9x)": {
        "car35": "Carp91/92 — 收支彙總、實收 vs 應收",
        "ars": "reports 只有營收/工單/技師/庫存",
        "gap": "SRS 明確排除總帳/稅務申報，可不做",
    },
}

print("\n" + "=" * 70)
print("功能缺口分析")
print("=" * 70)

for concept, info in CAR35_CONCEPTS.items():
    print(f"\n【{concept}】")
    print(f"  CAR35: {info['car35']}")
    print(f"  目前: {info['ars']}")
    print(f"  差距: {info['gap']}")

# 對照我們已覆蓋的部分
print("\n" + "=" * 70)
print("AutoRepair OS 已覆蓋 CAR35 的功能")
print("=" * 70)

covered = [
    ("客戶主檔", "customers 表 + 搜尋 + LINE 綁定 + 開工單入口", "Carp14/71"),
    ("車輛主檔", "vehicles 表 + 搜尋 + 里程預填", "Carp31/74"),
    ("工單核心", "work_orders + 狀態流轉 + 審批 + SOP", "Carp51"),
    ("估價/明細", "work_order_items（工資/零件/外包）+ 自動重算總額", "Carp32"),
    ("結帳", "status=paid + 扣料 + 累計消費 + 鎖定內容", "Carp52"),
    ("庫存現況", "inventory_items + 低水位警示", "Carp61/73"),
    ("庫存異動", "扣料/回補 + 調整記錄", "Carp62/84"),
    ("技師工資", "technician_worklogs + 佣金計算", "Carp41/42"),
    ("廠商", "suppliers 表（僅基本資料）", "Carp72/78"),
    ("收據列印", "receipt + print CSS", "Carp52"),
    ("LINE 推播", "Messaging API + Flex 估價單 + 客戶綁定", "（CAR35 無，現代加值）"),
    ("技師佣金報表", "technician/commissions 月度統計", "Carp42"),
    ("離線優先", "IndexedDB + SW + 背景重試", "（CAR35 無）"),
    ("排程與備份", "cron + VACUUM INTO + 30 天保留", "（CAR35 無）"),
]

for name, ars, ref in covered:
    print(f"  ✓ {name:14s} {ars:46s} (CAR35 {ref})")

print("\n" + "=" * 70)
print("建議優先順序")
print("=" * 70)
print("""
P0（營收核心，缺了會少賺錢）
  1. 保險公司 + 理賠工單
     - insurance_companies 表（公司名、統編、聯絡人、稅號）
     - work_orders 加 insurance_company_id / claim_no / claim_type
     - 估價單可切「保險理賠」模式（代客向保險公司請款）
     - CAR35 用 Company.Short + Insurance 欄位，這是車行最重要的營收來源

  2. 付款方式拆分
     - 現金 / 支票 / 信用卡 / 轉帳 / 應收
     - 報表可依支付方式分析，判斷資金流

P1（營運效率）
  3. 應收帳款明細
     - receivable 表（工單、金額、到期日、狀態、收款紀錄）
     - 支援分期，車行常見

  4. 廠商往來與採購
     - 採購單 → 入庫 → 應付帳款
     - 目前庫存只能手動調漲，無法追蹤進貨成本

P2（加值）
  5. 代客保管（Consign）
  6. 客戶生日關懷（LINE 行銷）
  7. 車輛排放/年檢提醒

不做（SRS 已排除）
  ✗ 總帳會計、稅務申報
  ✗ 分店管理（單店）
""")

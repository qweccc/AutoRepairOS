-- 041: 供應商應付帳款與分批付款
-- 應付金額為採購進貨或外包成立時的快照，來源唯一鍵確保重複流程不會重複入帳。
CREATE TABLE IF NOT EXISTS ap_payables (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_type TEXT NOT NULL CHECK (source_type IN ('purchase_receive', 'subcontract')),
    source_id INTEGER NOT NULL CHECK (source_id > 0),
    supplier_id INTEGER NOT NULL REFERENCES suppliers(id),
    amount REAL NOT NULL DEFAULT 0 CHECK (amount >= 0),
    paid_amount REAL NOT NULL DEFAULT 0 CHECK (paid_amount >= 0 AND paid_amount <= amount),
    balance REAL NOT NULL DEFAULT 0 CHECK (balance >= 0),
    due_date TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'partial', 'paid', 'void')),
    created_at TEXT NOT NULL DEFAULT (datetime('now','localtime')),
    paid_at TEXT,
    UNIQUE (source_type, source_id)
);
CREATE INDEX IF NOT EXISTS idx_ap_payables_supplier_due ON ap_payables(supplier_id, due_date);
CREATE INDEX IF NOT EXISTS idx_ap_payables_status ON ap_payables(status);

CREATE TABLE IF NOT EXISTS ap_payments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    payable_id INTEGER NOT NULL REFERENCES ap_payables(id),
    amount REAL NOT NULL CHECK (amount > 0),
    paid_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now','localtime'))
);
CREATE INDEX IF NOT EXISTS idx_ap_payments_payable ON ap_payments(payable_id, paid_at);

-- Seed existing source documents once during upgrade so the first AP view includes
-- already-received purchase orders and established, non-cancelled subcontract work.
WITH purchase_amounts AS (
    SELECT order_id, SUM(subtotal) AS amount
    FROM active_purchase_order_items
    GROUP BY order_id
), purchase_sources AS (
    SELECT p.id, p.supplier_id,
        COALESCE(pa.amount, p.total_amount, 0) AS amount,
        COALESCE(NULLIF(date(p.received_at), ''), NULLIF(date(p.order_date), ''),
            date(p.created_at), date('now','localtime')) AS issue_date,
        COALESCE(NULLIF(p.received_at, ''), NULLIF(p.order_date, ''), p.created_at,
            datetime('now','localtime')) AS created_at,
        COALESCE(s.payment_terms, '') AS payment_terms
    FROM purchase_orders p
    JOIN suppliers s ON s.id = p.supplier_id
    LEFT JOIN purchase_amounts pa ON pa.order_id = p.id
    WHERE p.status = 'received' AND p.deleted_at IS NULL
)
INSERT INTO ap_payables
    (source_type, source_id, supplier_id, amount, paid_amount, balance, due_date, status, created_at)
SELECT 'purchase_receive', id, supplier_id, amount, 0, amount,
    CASE payment_terms
        WHEN 'monthly' THEN date(issue_date, 'start of month', '+1 month', '-1 day')
        WHEN 'check30' THEN date(issue_date, '+30 days')
        WHEN 'check60' THEN date(issue_date, '+60 days')
        ELSE issue_date
    END, 'open', created_at
FROM purchase_sources
WHERE amount >= 0
ON CONFLICT(source_type, source_id) DO NOTHING;

WITH subcontract_sources AS (
    SELECT sc.id, sc.supplier_id, sc.cost_amount AS amount,
        COALESCE(date(sc.created_at), date('now','localtime')) AS issue_date,
        COALESCE(sc.created_at, datetime('now','localtime')) AS created_at,
        COALESCE(s.payment_terms, '') AS payment_terms
    FROM subcontracts sc
    JOIN suppliers s ON s.id = sc.supplier_id
    WHERE sc.supplier_id IS NOT NULL AND sc.cost_amount > 0
        AND sc.status != 'cancelled' AND sc.deleted_at IS NULL
)
INSERT INTO ap_payables
    (source_type, source_id, supplier_id, amount, paid_amount, balance, due_date, status, created_at)
SELECT 'subcontract', id, supplier_id, amount, 0, amount,
    CASE payment_terms
        WHEN 'monthly' THEN date(issue_date, 'start of month', '+1 month', '-1 day')
        WHEN 'check30' THEN date(issue_date, '+30 days')
        WHEN 'check60' THEN date(issue_date, '+60 days')
        ELSE issue_date
    END, 'open', created_at
FROM subcontract_sources
WHERE amount > 0
ON CONFLICT(source_type, source_id) DO NOTHING;

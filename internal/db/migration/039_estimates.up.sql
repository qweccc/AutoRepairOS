-- T2.9: Independent estimate drafts and their traceable work-order conversion.
-- Existing work_orders.status and work_order_items.work_order_id have restrictive
-- CHECK/NOT NULL constraints, so drafts use parallel tables with matching item fields.
CREATE TABLE IF NOT EXISTS estimates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    customer_id INTEGER NOT NULL,
    vehicle_id INTEGER NOT NULL,
    current_mileage INTEGER NOT NULL DEFAULT 0,
    customer_complaint TEXT NOT NULL DEFAULT '',
    discount_amount REAL NOT NULL DEFAULT 0,
    total_amount REAL NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','converted')),
    created_by INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now','localtime')),
    converted_work_order_id INTEGER UNIQUE,
    FOREIGN KEY (customer_id) REFERENCES customers(id),
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id),
    FOREIGN KEY (created_by) REFERENCES users(id),
    FOREIGN KEY (converted_work_order_id) REFERENCES work_orders(id)
);

CREATE TABLE IF NOT EXISTS estimate_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    estimate_id INTEGER NOT NULL,
    item_type TEXT NOT NULL CHECK(item_type IN ('labor','part','subcontract')),
    inventory_item_id INTEGER,
    description TEXT NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 1,
    unit_price REAL NOT NULL DEFAULT 0,
    subtotal REAL NOT NULL DEFAULT 0,
    primary_technician_id INTEGER,
    assistant_technician_id INTEGER,
    photo_url TEXT,
    FOREIGN KEY (estimate_id) REFERENCES estimates(id),
    FOREIGN KEY (inventory_item_id) REFERENCES inventory_items(id),
    FOREIGN KEY (primary_technician_id) REFERENCES users(id),
    FOREIGN KEY (assistant_technician_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_estimates_customer ON estimates(customer_id);
CREATE INDEX IF NOT EXISTS idx_estimates_vehicle ON estimates(vehicle_id);
CREATE INDEX IF NOT EXISTS idx_estimates_status ON estimates(status);
CREATE INDEX IF NOT EXISTS idx_estimate_items_estimate ON estimate_items(estimate_id);

ALTER TABLE work_orders ADD COLUMN original_estimate_id INTEGER REFERENCES estimates(id);
ALTER TABLE work_orders ADD COLUMN quoted_total_amount REAL NOT NULL DEFAULT 0;
ALTER TABLE work_orders ADD COLUMN quoted_discount_amount REAL NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX IF NOT EXISTS idx_work_orders_original_estimate
    ON work_orders(original_estimate_id) WHERE original_estimate_id IS NOT NULL;

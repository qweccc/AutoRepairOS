CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password TEXT NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('owner','counter','technician')),
    name TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS suppliers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    phone TEXT,
    contact_person TEXT,
    address TEXT,
    notes TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS customers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    phone TEXT NOT NULL UNIQUE,
    line_user_id TEXT,
    address TEXT,
    notification_preference TEXT DEFAULT 'line',
    notes TEXT,
    total_spent REAL DEFAULT 0,
    unpaid_amount REAL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS vehicles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    customer_id INTEGER NOT NULL,
    plate_number TEXT NOT NULL UNIQUE,
    brand_model TEXT NOT NULL,
    manufacture_year INTEGER,
    vin TEXT,
    engine_number TEXT,
    inspection_due_date DATE,
    next_maintenance_date DATE,
    next_maintenance_mileage INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (customer_id) REFERENCES customers(id)
);

CREATE TABLE IF NOT EXISTS inventory_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    supplier_id INTEGER,
    part_number TEXT NOT NULL UNIQUE,
    barcode TEXT,
    name TEXT NOT NULL,
    bin_location TEXT NOT NULL,
    cost_price REAL DEFAULT 0,
    selling_price REAL DEFAULT 0,
    stock_quantity INTEGER DEFAULT 0,
    min_safety_stock INTEGER DEFAULT 5,
    FOREIGN KEY (supplier_id) REFERENCES suppliers(id)
);

CREATE TABLE IF NOT EXISTS work_orders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    vehicle_id INTEGER NOT NULL,
    status TEXT CHECK(status IN ('pending','in_progress','waiting_parts','completed','paid')) DEFAULT 'pending',
    current_mileage INTEGER NOT NULL,
    customer_complaint TEXT,
    approval_status TEXT CHECK(approval_status IN ('not_required','pending','approved','rejected')) DEFAULT 'not_required',
    total_amount REAL DEFAULT 0,
    sop_completed BOOLEAN DEFAULT 0,
    customer_id INTEGER NOT NULL,
    technician_id INTEGER,
    notes TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id),
    FOREIGN KEY (customer_id) REFERENCES customers(id),
    FOREIGN KEY (technician_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS work_order_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_order_id INTEGER NOT NULL,
    item_type TEXT CHECK(item_type IN ('labor','part','subcontract')) NOT NULL,
    inventory_item_id INTEGER,
    description TEXT NOT NULL,
    quantity INTEGER DEFAULT 1,
    unit_price REAL DEFAULT 0,
    subtotal REAL DEFAULT 0,
    primary_technician_id INTEGER,
    assistant_technician_id INTEGER,
    photo_url TEXT,
    FOREIGN KEY (work_order_id) REFERENCES work_orders(id),
    FOREIGN KEY (inventory_item_id) REFERENCES inventory_items(id),
    FOREIGN KEY (primary_technician_id) REFERENCES users(id),
    FOREIGN KEY (assistant_technician_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_vehicles_customer ON vehicles(customer_id);
CREATE INDEX IF NOT EXISTS idx_work_orders_vehicle ON work_orders(vehicle_id);
CREATE INDEX IF NOT EXISTS idx_work_orders_status ON work_orders(status);
CREATE INDEX IF NOT EXISTS idx_work_orders_customer ON work_orders(customer_id);
CREATE INDEX IF NOT EXISTS idx_work_order_items_wo ON work_order_items(work_order_id);
CREATE INDEX IF NOT EXISTS idx_inventory_part_number ON inventory_items(part_number);

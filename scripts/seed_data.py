import sqlite3
import os
import json

# Database paths
DATABASE_PATH = r"C:\Users\fr\AutoRepairOS\data\test_seed.db"
LOG_FILE = r"C:\Users\fr\AutoRepairOS\scripts\seed_log.txt"

def log(message):
    with open(LOG_FILE, "a", encoding="utf-8") as f:
        f.write(f"{message}\n")
    print(message)

def seed_data():
    # Ensure directory exists
    os.makedirs(os.path.dirname(DATABASE_PATH), exist_ok=True)
    
    # Connect to the test database
    conn = sqlite3.connect(DATABASE_PATH)
    conn.isolation_level = None  # Disable autocommit for manual transaction control
    cursor = conn.cursor()

    try:
        log("Starting Seeding Process...")
        
        # 1. Create Tables (based on SRS.md and expected schema)
        # We use a simple structure first to ensure the script works.
        # Note: This will be updated if actual schema is different.
        
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS customers (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL,
                phone TEXT,
                email TEXT,
                address TEXT,
                membership_level TEXT DEFAULT 'Regular',
                created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
            )
        """)

        cursor.execute("""
            CREATE TABLE IF NOT EXISTS vehicles (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                customer_id INTEGER,
                make TEXT,
                model TEXT,
                year INTEGER,
                vin TEXT UNIQUE,
                license_plate TEXT,
                FOREIGN KEY(customer_id) REFERENCES customers(id)
            )
        """)

        cursor.execute("""
            CREATE TABLE IF NOT EXISTS inventory (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                part_number TEXT UNIQUE,
                name TEXT,
                category TEXT,
                bin_location TEXT,
                quantity INTEGER DEFAULT 0,
                unit_price REAL
            )
        """)

        cursor.execute("""
            CREATE TABLE IF NOT EXISTS work_orders (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                customer_id INTEGER,
                vehicle_id INTEGER,
                status TEXT CHECK(status IN ('Pending', 'Inspection', 'In Progress', 'Completed', 'Paid', 'Cancelled')),
                description TEXT,
                total_price REAL DEFAULT 0.0,
                created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                FOREIGN KEY(customer_id) REFERENCES customers(id),
                FOREIGN KEY(vehicle_id) REFERENCES vehicles(id)
            )
        """)

        # 2. Insert Mock Data
        log("Inserting mock customers...")
        customers = [
            ("John Doe", "0912-345-678", "john@example.com", "Taipei City", "VIP"),
            ("Jane Smith", "0987-654-321", "jane@test.com", "New Taipei City", "Regular"),
            ("李華", "0936-123-4567", "lihua@china.cn", "Taichung City", "Regular"),
            ("整備公司A", "0876-543-210", "admin@company.tw", "Kaohsiung City", "Corporate")
        ]
        cursor.execute("INSERT INTO customers (name, phone, email, address, membership_level) VALUES (?, ?, ?, ?, ?)", customers)

        log("Inserting mock vehicles...")
        # Assuming customer IDs 1, 2, 3, 4 were created
        vehicles = [
            (1, "Toyota", "Camry", 2022, "VIN123456789", "ABC-1234"),
            (2, "Tesla", "Model 3", 2023, "VIN987654321", "XYZ-9999"),
            (3, "Honda", "Civic", 2019, "VIN456789123", "DEF-5566"),
            (4, "Ford", "F-150", 2021, "VIN1122334455", "GHI-7788")
        ]
        cursor.execute("INSERT INTO vehicles (customer_id, make, model, year, vin, license_plate) VALUES (?, ?, ?, ?, ?, ?)", vehicles)

        log("Inserting mock inventory...")
        parts = [
            ("P001", "Oil Filter", "Engine", "A-01", 50, 150.0),
            ("P002", "Brake Pad - Front", "Brakes", "B-02", 20, 800.0),
            ("P003", "Spark Plug", "Ignition", "C-01", 100, 50.0),
            ("P004", "Battery", "Electrical", "D-01", 5, 1200.0),
            ("P005", "Air Filter", "Engine", "A-01", 30, 200.0)
        ]
        cursor.execute("INSERT INTO inventory (part_number, name, category, bin_location, quantity, unit_price) VALUES (?, ?, ?, ?, ?, ?)", parts)

        log("Inserting mock work orders...")
        # Status flow: Pending(1), Inspection(2), In Progress(3), Completed(4), Paid(5)
        orders = [
            (1, 1, 1, 'Pending', 'Oil change request', 0.0),
            (2, 2, 2, 'Inspection', 'Brake squeaking noise', 0.0),
            (3, 3, 3, 'In Progress', 'Engine overhaul', 5000.0),
            (4, 4, 4, 'Paid', 'Routine maintenance', 1200.0),
            (5, 1, 1, 'Completed', 'Last month fix', 300.0)
        ]
        cursor.execute("INSERT INTO work_orders (customer_id, vehicle_id, status, description, total_price) VALUES (?, ?, ?, ?, ?)", orders)

        conn.commit()
        log("SUCCESS: Data seeding completed.")

    except Exception as e:
        conn.rollback()
        log(f"ERROR: Seeding failed: {str(e)}")
    finally:
        conn.close()

if __name__ == "__main__":
    seed_data()

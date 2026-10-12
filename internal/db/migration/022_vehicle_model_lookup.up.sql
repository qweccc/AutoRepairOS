-- 022: 車輛品牌車型代碼表（vehicle_model）
--
-- 為什麼需要：brand_model 原本是自由輸入，實務上會出現同一台車寫成
--   「Toyota Altis」「TOYOTA ALTIS」「altis」三種，導致
--     - 經營分析的「高頻維修車款」分組散掉（同款車算三台）
--     - 車輛列表搜尋要試各種寫法
--     - 無法統計「這個車款進廠幾次」
--
-- 做法：新增 lookup category 'vehicle_model'，車輛表單改為下拉選單。
--
-- 設計要點：
--   - code 存「廠牌 車型」完整字串（如 'Toyota Altis'），因為車輛只有
--     一個 brand_model 欄位，沒有獨立的 make/model 可分開存。用完整字串
--     當 code 才能直接寫入 brand_model 而不需要轉換。
--   - 保留自由輸入的逃生口：車款太多無法全列（新車每月出），因此下拉
--     旁邊保留「其他」選項，選了才展開文字輸入框。這樣既有資料不會
--     因為選單裡沒有而被強制改寫。
--   - 已有車輛的 brand_model 不會被回填（可能是舊系統的特殊寫法），
--     只在編輯時於下拉中標示為「（現有值）」讓使用者決定是否正規化。

INSERT OR IGNORE INTO lookup_items (category, code, label, sort_order) VALUES
    ('vehicle_model', 'Toyota Altis', 'Toyota Altis', 1),
    ('vehicle_model', 'Toyota Camry', 'Toyota Camry', 2),
    ('vehicle_model', 'Toyota Corolla', 'Toyota Corolla', 3),
    ('vehicle_model', 'Toyota RAV4', 'Toyota RAV4', 4),
    ('vehicle_model', 'Toyota Yaris', 'Toyota Yaris', 5),
    ('vehicle_model', 'Toyota Vios', 'Toyota Vios', 6),
    ('vehicle_model', 'Honda City', 'Honda City', 7),
    ('vehicle_model', 'Honda CR-V', 'Honda CR-V', 8),
    ('vehicle_model', 'Honda Fit', 'Honda Fit', 9),
    ('vehicle_model', 'Honda HR-V', 'Honda HR-V', 10),
    ('vehicle_model', 'Mazda 3', 'Mazda 3', 11),
    ('vehicle_model', 'Mazda CX-5', 'Mazda CX-5', 12),
    ('vehicle_model', 'Mazda CX-30', 'Mazda CX-30', 13),
    ('vehicle_model', 'Nissan Sentra', 'Nissan Sentra', 14),
    ('vehicle_model', 'Nissan Kicks', 'Nissan Kicks', 15),
    ('vehicle_model', 'Nissan X-Trail', 'Nissan X-Trail', 16),
    ('vehicle_model', 'Hyundai Tucson', 'Hyundai Tucson', 17),
    ('vehicle_model', 'Hyundai Elantra', 'Hyundai Elantra', 18),
    ('vehicle_model', 'Kia Sportage', 'Kia Sportage', 19),
    ('vehicle_model', 'Kia Carnival', 'Kia Carnival', 20),
    ('vehicle_model', 'Ford Focus', 'Ford Focus', 21),
    ('vehicle_model', 'Ford Kuga', 'Ford Kuga', 22),
    ('vehicle_model', 'Ford Ranger', 'Ford Ranger', 23),
    ('vehicle_model', 'Volkswagen Golf', 'Volkswagen Golf', 24),
    ('vehicle_model', 'Volkswagen Tiguan', 'Volkswagen Tiguan', 25),
    ('vehicle_model', 'BMW 3 Series', 'BMW 3 Series', 26),
    ('vehicle_model', 'BMW 5 Series', 'BMW 5 Series', 27),
    ('vehicle_model', 'BMW X3', 'BMW X3', 28),
    ('vehicle_model', 'BMW X5', 'BMW X5', 29),
    ('vehicle_model', 'Mercedes-Benz C-Class', 'Mercedes-Benz C-Class', 30),
    ('vehicle_model', 'Mercedes-Benz E-Class', 'Mercedes-Benz E-Class', 31),
    ('vehicle_model', 'Mercedes-Benz GLC', 'Mercedes-Benz GLC', 32),
    ('vehicle_model', 'Audi A4', 'Audi A4', 33),
    ('vehicle_model', 'Audi Q5', 'Audi Q5', 34),
    ('vehicle_model', 'Lexus UX', 'Lexus UX', 35),
    ('vehicle_model', 'Lexus NX', 'Lexus NX', 36),
    ('vehicle_model', 'Tesla Model 3', 'Tesla Model 3', 37),
    ('vehicle_model', 'Tesla Model Y', 'Tesla Model Y', 38),
    ('vehicle_model', 'MG HS', 'MG HS', 39),
    ('vehicle_model', 'Subaru Forester', 'Subaru Forester', 40),
    ('vehicle_model', '其他', '其他（手動輸入）', 99);

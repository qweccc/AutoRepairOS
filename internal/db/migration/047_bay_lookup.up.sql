-- 將預約工位移至可由代碼對照表維護的 lookup。
INSERT OR IGNORE INTO lookup_items (category, code, label, sort_order) VALUES
    ('bay', 'LIFT', '頂高機', 1),
    ('bay', 'ALIGN', '四輪定位機', 2),
    ('bay', 'BODY', '鈑金校正機', 3),
    ('bay', 'PAINT', '烤漆房', 4),
    ('bay', 'TIRE', '拆胎機／平衡機', 5),
    ('bay', 'DIAG', '電腦診斷位', 6);

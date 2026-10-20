-- T2.21: 客戶生日（生日關懷用，FR-NOTI-03）
-- customers 表原本沒有生日欄位（只有 users/員工有），生日關懷無從判斷。
ALTER TABLE customers ADD COLUMN birthday TEXT DEFAULT '';

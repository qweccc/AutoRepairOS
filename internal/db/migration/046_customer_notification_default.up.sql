-- 匯入資料與新客戶預設不主動通知，綁定通知管道後再由使用者明確開啟。
UPDATE customers
SET notification_preference = 'none'
WHERE COALESCE(line_user_id, '') = ''
  AND COALESCE(telegram_user_id, '') = '';

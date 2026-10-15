-- 025: 進貨單品項帶入「猜到的零件類別」
--
-- 為什麼需要：進貨單拍照辨識時，GuessCategory() 已依品名猜出零件類別
-- （引擎/輪胎/電機音響...）。原本這個猜測值只存在前端畫面，確認進貨時
-- 自動建檔新零件就沒有類別可用，零件會變成「未分類」，之後統計與篩選都不準。
--
-- 這裡把猜測結果存進 purchase_order_items.category，確認進貨時
-- （ReceivePurchaseOrder → autoCreatePartFromItem）帶到新建的 inventory_items.category。
ALTER TABLE purchase_order_items ADD COLUMN category TEXT DEFAULT '';

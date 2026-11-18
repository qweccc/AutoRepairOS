-- FR-WO-05：照片標記要能關聯工單服務明細（原本後端驗證了 item_id 卻沒有存）。
ALTER TABLE photo_annotations ADD COLUMN item_id INTEGER;

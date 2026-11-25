-- Gói thức ăn khởi đầu cho các nông trại đã có trước khi có kho: các vụ thu hoạch trước đó đã trả
-- thẳng bằng xu nên kho trống, vật nuôi không có gì để ăn. Nông trại tạo mới nhận gói này trong code.
INSERT INTO farm_stock (user_id, item, qty)
SELECT f.user_id, s.item, s.qty
FROM farms f
CROSS JOIN (
    SELECT 'rau_muong' AS item, 6 AS qty
    UNION ALL SELECT 'ngo', 3
    UNION ALL SELECT 'ca_rot', 4
    UNION ALL SELECT 'ca_chua', 3
) s
WHERE true
ON CONFLICT (user_id, item) DO UPDATE SET qty = qty + excluded.qty;

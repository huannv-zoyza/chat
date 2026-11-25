DROP TABLE IF EXISTS farm_orders;
DROP TABLE IF EXISTS farm_animals;
DROP TABLE IF EXISTS farm_buildings;
DROP TABLE IF EXISTS farm_stock;
ALTER TABLE farms DROP COLUMN storage_level;
-- Phần 1 chỉ có tối đa 12 ô: bỏ các ô mở rộng thêm. (stolen giữ nguyên số đơn vị, không đổi lại được ra xu.)
DELETE FROM farm_plots WHERE idx >= 12;
UPDATE farms SET plots = 12 WHERE plots > 12;

-- Chỗ đặt công trình tự do (xưởng chế biến...) trên bản đồ nông trại, theo ô (specs/FARM_SPEC.md §10).
-- NULL: chỗ mặc định của công trình đó.
ALTER TABLE farm_buildings ADD COLUMN x INTEGER;
ALTER TABLE farm_buildings ADD COLUMN y INTEGER;

ALTER TABLE reservations DROP COLUMN IF EXISTS group_id;
ALTER TABLE v6_reservations DROP COLUMN IF EXISTS group_id;
DROP TABLE IF EXISTS reservation_groups;

-- Ensure nullable string columns have non-NULL defaults so Go scans into
-- plain string fields never fail, especially after migrating data from
-- external DHCP servers.

UPDATE reservations SET hostname = COALESCE(hostname, '') WHERE hostname IS NULL;
ALTER TABLE reservations ALTER COLUMN hostname SET NOT NULL;
ALTER TABLE reservations ALTER COLUMN hostname SET DEFAULT '';

UPDATE reservations SET description = COALESCE(description, '') WHERE description IS NULL;
ALTER TABLE reservations ALTER COLUMN description SET NOT NULL;
ALTER TABLE reservations ALTER COLUMN description SET DEFAULT '';

UPDATE v6_reservations SET hostname = COALESCE(hostname, '') WHERE hostname IS NULL;
ALTER TABLE v6_reservations ALTER COLUMN hostname SET NOT NULL;
ALTER TABLE v6_reservations ALTER COLUMN hostname SET DEFAULT '';

UPDATE v6_reservations SET description = COALESCE(description, '') WHERE description IS NULL;
ALTER TABLE v6_reservations ALTER COLUMN description SET NOT NULL;
ALTER TABLE v6_reservations ALTER COLUMN description SET DEFAULT '';

UPDATE leases SET hostname = COALESCE(hostname, '') WHERE hostname IS NULL;
ALTER TABLE leases ALTER COLUMN hostname SET NOT NULL;
ALTER TABLE leases ALTER COLUMN hostname SET DEFAULT '';

UPDATE leases SET client_id = COALESCE(client_id, '') WHERE client_id IS NULL;
ALTER TABLE leases ALTER COLUMN client_id SET NOT NULL;
ALTER TABLE leases ALTER COLUMN client_id SET DEFAULT '';

UPDATE v6_leases SET hostname = COALESCE(hostname, '') WHERE hostname IS NULL;
ALTER TABLE v6_leases ALTER COLUMN hostname SET NOT NULL;
ALTER TABLE v6_leases ALTER COLUMN hostname SET DEFAULT '';

UPDATE mac_blacklist SET reason = COALESCE(reason, '') WHERE reason IS NULL;
ALTER TABLE mac_blacklist ALTER COLUMN reason SET NOT NULL;
ALTER TABLE mac_blacklist ALTER COLUMN reason SET DEFAULT '';

UPDATE reservation_groups SET description = COALESCE(description, '') WHERE description IS NULL;
ALTER TABLE reservation_groups ALTER COLUMN description SET NOT NULL;
ALTER TABLE reservation_groups ALTER COLUMN description SET DEFAULT '';

UPDATE ha_nodes SET listen_addr = COALESCE(listen_addr, '') WHERE listen_addr IS NULL;
ALTER TABLE ha_nodes ALTER COLUMN listen_addr SET NOT NULL;
ALTER TABLE ha_nodes ALTER COLUMN listen_addr SET DEFAULT '';

UPDATE ha_nodes SET version = COALESCE(version, '') WHERE version IS NULL;
ALTER TABLE ha_nodes ALTER COLUMN version SET NOT NULL;
ALTER TABLE ha_nodes ALTER COLUMN version SET DEFAULT '';

UPDATE scopes SET domain_name = COALESCE(domain_name, '') WHERE domain_name IS NULL;
ALTER TABLE scopes ALTER COLUMN domain_name SET NOT NULL;
ALTER TABLE scopes ALTER COLUMN domain_name SET DEFAULT '';

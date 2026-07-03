-- Normalize IPv4-mapped IPv6 addresses to plain IPv4.
-- These addresses are stored with an ::ffff: prefix in the inet column and must
-- be rewritten so that IPv4 comparisons and unique constraints work correctly.

UPDATE scopes SET start_ip = host(start_ip)::inet WHERE text(start_ip) LIKE '::ffff:%';
UPDATE scopes SET end_ip   = host(end_ip)::inet   WHERE text(end_ip)   LIKE '::ffff:%';

UPDATE reservations SET ip_addr = host(ip_addr)::inet WHERE text(ip_addr) LIKE '::ffff:%';
UPDATE v6_reservations SET ip_addr = host(ip_addr)::inet WHERE text(ip_addr) LIKE '::ffff:%';

UPDATE leases SET ip_addr = host(ip_addr)::inet WHERE text(ip_addr) LIKE '::ffff:%';
UPDATE v6_leases SET ip_addr = host(ip_addr)::inet WHERE text(ip_addr) LIKE '::ffff:%';

UPDATE v6_prefixes SET prefix = host(prefix)::inet WHERE text(prefix) LIKE '::ffff:%';

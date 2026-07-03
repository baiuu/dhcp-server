-- Normalize remaining IPv4-mapped IPv6 addresses (::ffff:...) across all
-- inet/cidr columns, including arrays. PostgreSQL's host() strips the netmask
-- but keeps the mapped representation, so for inet columns we use host() then
-- remove the ::ffff: prefix. For cidr columns we operate on text() which
-- preserves the mask.

UPDATE scopes SET
    subnet = regexp_replace(text(subnet), '^::ffff:', '')::cidr,
    prefix = CASE WHEN prefix IS NULL THEN NULL ELSE regexp_replace(text(prefix), '^::ffff:', '')::cidr END,
    start_ip = regexp_replace(host(start_ip), '^::ffff:', '')::inet,
    end_ip = regexp_replace(host(end_ip), '^::ffff:', '')::inet,
    gateway = COALESCE((
        SELECT array_agg(regexp_replace(host(x), '^::ffff:', '')::inet)
        FROM unnest(gateway) x
    ), '{}'::inet[]),
    dns = COALESCE((
        SELECT array_agg(regexp_replace(host(x), '^::ffff:', '')::inet)
        FROM unnest(dns) x
    ), '{}'::inet[]),
    excluded_ips = COALESCE((
        SELECT array_agg(regexp_replace(host(x), '^::ffff:', '')::inet)
        FROM unnest(excluded_ips) x
    ), '{}'::inet[])
WHERE text(subnet) LIKE '::ffff:%'
   OR text(prefix) LIKE '::ffff:%'
   OR text(start_ip) LIKE '::ffff:%'
   OR text(end_ip) LIKE '::ffff:%'
   OR EXISTS (SELECT 1 FROM unnest(gateway) x WHERE text(x) LIKE '::ffff:%')
   OR EXISTS (SELECT 1 FROM unnest(dns) x WHERE text(x) LIKE '::ffff:%')
   OR EXISTS (SELECT 1 FROM unnest(excluded_ips) x WHERE text(x) LIKE '::ffff:%');

UPDATE reservations    SET ip_addr = regexp_replace(host(ip_addr), '^::ffff:', '')::inet WHERE text(ip_addr) LIKE '::ffff:%';
UPDATE leases          SET ip_addr = regexp_replace(host(ip_addr), '^::ffff:', '')::inet WHERE text(ip_addr) LIKE '::ffff:%';
UPDATE v6_reservations SET ip_addr = regexp_replace(host(ip_addr), '^::ffff:', '')::inet WHERE text(ip_addr) LIKE '::ffff:%';
UPDATE v6_leases       SET ip_addr = regexp_replace(host(ip_addr), '^::ffff:', '')::inet WHERE text(ip_addr) LIKE '::ffff:%';
UPDATE v6_prefixes     SET prefix  = regexp_replace(text(prefix), '^::ffff:', '')::cidr  WHERE text(prefix)  LIKE '::ffff:%';

-- Normalize IPv4 addresses stored in inet columns to a canonical form.
-- PostgreSQL may represent IPv4 addresses as IPv4-mapped IPv6 (::ffff:x.x.x.x)
-- depending on how they were inserted.  host() returns the plain address text,
-- and casting back to inet gives the canonical IPv4 form.
-- family() = 4 limits the update to IPv4 addresses so IPv6 prefixes are untouched.

UPDATE scopes SET start_ip = host(start_ip)::inet WHERE family(start_ip) = 4;
UPDATE scopes SET end_ip   = host(end_ip)::inet   WHERE family(end_ip)   = 4;

UPDATE reservations SET ip_addr = host(ip_addr)::inet WHERE family(ip_addr) = 4;
UPDATE v6_reservations SET ip_addr = host(ip_addr)::inet WHERE family(ip_addr) = 4;

UPDATE leases SET ip_addr = host(ip_addr)::inet WHERE family(ip_addr) = 4;
UPDATE v6_leases SET ip_addr = host(ip_addr)::inet WHERE family(ip_addr) = 4;

UPDATE v6_prefixes SET prefix = host(prefix)::inet WHERE family(prefix) = 4;

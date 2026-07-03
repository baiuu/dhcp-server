export const PAGE_SIZE = 20

export function formatDate(iso) {
  if (!iso) return '-'
  return new Date(iso).toLocaleString('zh-CN')
}

export function parseIPRangeSize(start, end) {
  if (!start || !end) return 0
  if (start.includes(':')) return 1000
  const a = start.split('.').map(Number)
  const b = end.split('.').map(Number)
  return Math.max(1, (b[0] - a[0]) * 16777216 + (b[1] - a[1]) * 65536 + (b[2] - a[2]) * 256 + (b[3] - a[3]) + 1)
}

export function parseCommaIPs(str) {
  return str.split(',').map(s => s.trim()).filter(Boolean)
}

export function isValidMAC(mac) {
  return /^([0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}$/.test(mac)
}

export function isValidDUID(duid) {
  return duid && duid.length >= 6
}

export function isStandardMAC(q) {
  return isValidMAC(q)
}

export function isLeaseV6(lease) {
  return lease.duid !== undefined
}

export function isReservationV6(reservation) {
  return reservation.duid !== undefined
}

export function formatHex(str) {
  if (!str) return '-'
  if (/[^0-9a-fA-F]/.test(str) || str.length % 2 !== 0) return str
  return str.match(/.{1,2}/g).join(':').toLowerCase()
}

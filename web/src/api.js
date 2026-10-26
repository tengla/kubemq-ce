// Calls one of the dashboard actions handled by POST /api/request.
export async function apiRequest(type, data) {
  const response = await fetch('/api/request', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ type, data }),
  })
  const body = await response.json().catch(() => null)
  if (!response.ok || !body || body.error) throw new Error(body?.error_string || `HTTP ${response.status}`)
  return body.data
}

export function socketUrl(path, params = {}) {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const query = new URLSearchParams(Object.entries(params).filter(([, v]) => v !== '' && v != null)).toString()
  return `${protocol}//${location.host}${path}${query ? `?${query}` : ''}`
}

// The server stores a JSON object or array as JSON and anything else as text,
// so a body that parses as one is sent as structured data.
export function parseBodyInput(text) {
  const trimmed = text.trim()
  if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
    try {
      return JSON.parse(trimmed)
    } catch {
      // Not valid JSON; send it as text.
    }
  }
  return text
}

export function formatBody(body) {
  if (body == null) return ''
  return typeof body === 'string' ? body : JSON.stringify(body, null, 2)
}

// "a=1, b=2" -> "a=1,b=2", or throws if a pair is malformed.
export function normalizeTags(text) {
  const parts = text
    .split(',')
    .map((p) => p.trim())
    .filter(Boolean)
  for (const part of parts) {
    if (part.split('=').length !== 2) throw new Error(`Tag "${part}" must look like key=value`)
  }
  return parts.join(',')
}

// Message timestamps arrive in nanoseconds, milliseconds or seconds
// depending on the endpoint.
export function toDate(value) {
  if (!value) return null
  if (typeof value === 'string') return new Date(value.replace(' ', 'T'))
  if (value > 1e15) return new Date(value / 1e6)
  if (value > 1e12) return new Date(value)
  return new Date(value * 1000)
}

const timeFormat = new Intl.DateTimeFormat([], { dateStyle: 'medium', timeStyle: 'medium' })

export function formatTime(value) {
  const date = toDate(value)
  return date && !Number.isNaN(date.getTime()) ? timeFormat.format(date) : ''
}

export function clientId(prefix) {
  return `${prefix}-${Math.random().toString(36).slice(2, 10)}`
}

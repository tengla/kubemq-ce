import { reactive } from 'vue'

// Dashboard sign-in. When the broker has authentication enabled, the
// dashboard asks for a JWT (from `kubemq token`). The server keeps it in an
// HttpOnly cookie, so this module only tracks whether the session is valid.
export const session = reactive({
  checked: false,
  enabled: false,
  authenticated: true,
  subject: '',
  expiresAt: null,
})

function apply(data) {
  session.checked = true
  session.enabled = Boolean(data?.enabled)
  session.authenticated = Boolean(data?.authenticated)
  session.subject = data?.subject ?? ''
  session.expiresAt = data?.expiresAt ?? null
}

async function call(path, init) {
  const response = await fetch(path, { cache: 'no-store', ...init })
  const body = await response.json().catch(() => null)
  if (!response.ok || !body || body.error) throw new Error(body?.error_string || `HTTP ${response.status}`)
  return body.data
}

export async function loadSession() {
  try {
    apply(await call('/api/auth/session'))
  } catch {
    // Broker unreachable: show the dashboard, which reports that itself.
    session.checked = true
  }
}

export async function login(token) {
  apply(
    await call('/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token }),
    }),
  )
}

export async function logout() {
  apply(await call('/api/auth/logout', { method: 'POST' }))
}

export const needsLogin = () => session.enabled && !session.authenticated

// Called when the API answers 401: the token expired, the server's key
// changed, or the broker restarted with authentication on. The next render
// shows the login page.
export function sessionExpired() {
  session.enabled = true
  session.authenticated = false
}

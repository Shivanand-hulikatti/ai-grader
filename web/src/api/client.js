const rawBase = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8080'
const BASE = rawBase.replace(/\/$/, '')

let refreshPromise = null

function getToken() {
  return localStorage.getItem('access_token')
}

function getRefreshToken() {
  return localStorage.getItem('refresh_token')
}

function clearAuthStorage() {
  localStorage.removeItem('access_token')
  localStorage.removeItem('refresh_token')
  localStorage.removeItem('user')
  window.dispatchEvent(new Event('auth:expired'))
}

async function refreshAccessToken() {
  if (refreshPromise) return refreshPromise

  refreshPromise = (async () => {
    const token = getRefreshToken()
    if (!token) throw new Error('Session expired. Please sign in again.')

    const res = await fetch(BASE + '/auth/refresh', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: token }),
    })

    const data = await res.json().catch(() => ({}))
    if (!res.ok || !data.access_token) {
      clearAuthStorage()
      const err = new Error(data.message || 'Session expired. Please sign in again.')
      err.status = res.status
      err.code = data.error || 'refresh_failed'
      throw err
    }

    localStorage.setItem('access_token', data.access_token)
    if (data.refresh_token) localStorage.setItem('refresh_token', data.refresh_token)
    if (data.user) localStorage.setItem('user', JSON.stringify(data.user))
    return data.access_token
  })().finally(() => {
    refreshPromise = null
  })

  return refreshPromise
}

async function request(path, options = {}, retryOnAuthFail = true) {
  const headers = { ...options.headers }
  if (!(options.body instanceof FormData) && !headers['Content-Type']) {
    headers['Content-Type'] = 'application/json'
  }

  const token = getToken()
  if (token) headers['Authorization'] = `Bearer ${token}`

  const res = await fetch(BASE + path, { ...options, headers })
  const data = await res.json().catch(() => ({}))

  if (
    res.status === 401 &&
    retryOnAuthFail &&
    !path.startsWith('/auth/') &&
    (data.error === 'token_expired' || data.error === 'invalid_token' || data.error === 'missing_auth_header')
  ) {
    await refreshAccessToken()
    return request(path, options, false)
  }

  if (!res.ok) {
    const err = new Error(data.message || 'Request failed')
    err.status = res.status
    err.code = data.error
    throw err
  }
  return data
}

// Auth
export const authApi = {
  register: (body) => request('/auth/register', { method: 'POST', body: JSON.stringify(body) }),
  login: (body) => request('/auth/login', { method: 'POST', body: JSON.stringify(body) }),
  refresh: (body) => request('/auth/refresh', { method: 'POST', body: JSON.stringify(body) }),
}

// Upload
export const uploadApi = {
  upload: (formData) => request('/upload', { method: 'POST', body: formData }),
}

// Results
export const resultsApi = {
  list: (limit = 20, offset = 0) =>
    request(`/results?limit=${limit}&offset=${offset}`),
  get: (id) => request(`/results/${id}`),
}

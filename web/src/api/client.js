const BASE = process.env.VITE_API_BASE_URL || 'http://localhost:8080/api'

function getToken() {
  return localStorage.getItem('access_token')
}

async function request(path, options = {}) {
  const headers = { 'Content-Type': 'application/json', ...options.headers }
  const token = getToken()
  if (token) headers['Authorization'] = `Bearer ${token}`

  const res = await fetch(BASE + path, { ...options, headers })
  const data = await res.json().catch(() => ({}))

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
  upload: (formData) => {
    const token = getToken()
    return fetch(BASE + '/upload', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
      body: formData,
    }).then(async (res) => {
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        const err = new Error(data.message || 'Upload failed')
        err.status = res.status
        throw err
      }
      return data
    })
  },
}

// Results
export const resultsApi = {
  list: (limit = 20, offset = 0) =>
    request(`/results?limit=${limit}&offset=${offset}`),
  get: (id) => request(`/results/${id}`),
}

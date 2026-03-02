import { createContext, useContext, useState, useCallback } from 'react'
import { authApi } from '../api/client'

const AuthContext = createContext(null)

function readUserFromStorage() {
  try {
    const stored = localStorage.getItem('user')
    return stored ? JSON.parse(stored) : null
  } catch (_) {
    return null
  }
}

export function AuthProvider({ children }) {
  // Lazy initializer reads localStorage synchronously on first render —
  // this eliminates the loading flash that caused "not authenticated" on refresh.
  const [user, setUser] = useState(readUserFromStorage)
  const [loading] = useState(false)

  const login = useCallback(async (email, password) => {
    const data = await authApi.login({ email, password })
    localStorage.setItem('access_token', data.access_token)
    localStorage.setItem('refresh_token', data.refresh_token)
    localStorage.setItem('user', JSON.stringify(data.user))
    setUser(data.user)
    return data
  }, [])

  const register = useCallback(async (email, password, full_name) => {
    const data = await authApi.register({ email, password, full_name })
    localStorage.setItem('access_token', data.access_token)
    localStorage.setItem('refresh_token', data.refresh_token)
    localStorage.setItem('user', JSON.stringify(data.user))
    setUser(data.user)
    return data
  }, [])

  const logout = useCallback(() => {
    localStorage.removeItem('access_token')
    localStorage.removeItem('refresh_token')
    localStorage.removeItem('user')
    setUser(null)
  }, [])

  return (
    <AuthContext.Provider value={{ user, loading, login, register, logout }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  return useContext(AuthContext)
}

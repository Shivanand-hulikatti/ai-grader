import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'

export default function LoginPage() {
  const { login } = useAuth()
  const navigate = useNavigate()
  const [form, setForm] = useState({ email: '', password: '' })
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  async function handleSubmit(e) {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      await login(form.email, form.password)
      navigate('/dashboard')
    } catch (err) {
      setError(err.message || 'Login failed')
    } finally {
      setLoading(false)
    }
  }

  function set(field) {
    return (e) => setForm((f) => ({ ...f, [field]: e.target.value }))
  }

  return (
    <div className="min-h-screen bg-stone-50 flex flex-col items-center justify-center px-4">
      <div className="w-full max-w-sm animate-slide-up">
        {/* Wordmark */}
        <div className="mb-10 text-center">
          <h1 className="font-serif text-3xl text-stone-900">AI Grader</h1>
          <p className="mt-1.5 text-sm text-stone-400">Automated paper evaluation</p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="label">Email</label>
            <input
              type="email"
              placeholder="you@example.com"
              value={form.email}
              onChange={set('email')}
              required
              autoFocus
              className="input-field"
            />
          </div>

          <div>
            <label className="label">Password</label>
            <input
              type="password"
              placeholder="••••••••"
              value={form.password}
              onChange={set('password')}
              required
              className="input-field"
            />
          </div>

          {error && (
            <p className="text-xs text-red-500 bg-red-50 border border-red-100 rounded px-3 py-2">
              {error}
            </p>
          )}

          <button type="submit" disabled={loading} className="btn-primary w-full mt-2">
            {loading ? 'Signing in…' : 'Sign in'}
          </button>
        </form>

        <div className="mt-4 rounded border border-stone-200 bg-white px-3 py-2 text-xs text-stone-500">
          <p>Email: test@email.com</p>
          <p>Password: test1234</p>
        </div>

        <p className="mt-6 text-center text-sm text-stone-400">
          No account?{' '}
          <Link to="/register" className="text-stone-700 hover:text-stone-900 underline underline-offset-2">
            Create one
          </Link>
        </p>
      </div>
    </div>
  )
}

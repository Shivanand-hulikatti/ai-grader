import { Link, useNavigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'

export default function Layout({ children }) {
  const { user, logout } = useAuth()
  const navigate = useNavigate()

  function handleLogout() {
    logout()
    navigate('/login')
  }

  return (
    <div className="min-h-screen flex flex-col">
      {/* Nav */}
      <header className="border-b border-stone-200 bg-stone-50/80 backdrop-blur-sm sticky top-0 z-10">
        <div className="max-w-4xl mx-auto px-6 h-14 flex items-center justify-between">
          <Link to="/dashboard" className="font-serif text-lg text-stone-900 tracking-tight">
            AI Grader
          </Link>
          <nav className="flex items-center gap-5">
            <Link
              to="/dashboard"
              className="text-sm text-stone-500 hover:text-stone-900 transition-colors"
            >
              Results
            </Link>
            <Link
              to="/app/upload"
              className="text-sm text-stone-500 hover:text-stone-900 transition-colors"
            >
              Upload
            </Link>
            <div className="h-4 w-px bg-stone-200" />
            <span className="text-xs text-stone-400">{user?.full_name}</span>
            <button
              onClick={handleLogout}
              className="text-xs text-stone-400 hover:text-stone-700 transition-colors"
            >
              Sign out
            </button>
          </nav>
        </div>
      </header>

      {/* Content */}
      <main className="flex-1 max-w-4xl mx-auto w-full px-6 py-10 animate-slide-up">
        {children}
      </main>

      {/* Footer */}
      <footer className="border-t border-stone-100 py-5">
        <div className="max-w-4xl mx-auto px-6 flex items-center justify-between">
          <span className="text-xs text-stone-300">AI Grader © 2026</span>
          <span className="text-xs text-stone-300">Automated paper evaluation</span>
        </div>
      </footer>
    </div>
  )
}

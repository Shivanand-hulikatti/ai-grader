import { Link } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'

function CheckIcon() {
  return (
    <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2} aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
    </svg>
  )
}

function BoltIcon() {
  return (
    <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.8} aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M13 2L4 14h7l-1 8 10-14h-7l0-6z" />
    </svg>
  )
}

function RuleIcon() {
  return (
    <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.8} aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M9 17h6M9 13h6M9 9h6" />
      <path strokeLinecap="round" strokeLinejoin="round" d="M5 21h14a1 1 0 001-1V4a1 1 0 00-1-1H8l-4 4v13a1 1 0 001 1z" />
    </svg>
  )
}

function BarsIcon() {
  return (
    <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.8} aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M4 19h16M7 15V9m5 6V5m5 10v-4" />
    </svg>
  )
}

function Feature({ icon, title, text }) {
  return (
    <article className="card px-5 py-5 hover:bg-stone-50 transition-colors">
      <div className="w-9 h-9 rounded bg-stone-100 border border-stone-200 flex items-center justify-center text-stone-600">
        {icon}
      </div>
      <h3 className="mt-4 font-serif text-lg text-stone-900">{title}</h3>
      <p className="mt-2 text-sm text-stone-500 leading-relaxed">{text}</p>
    </article>
  )
}

export default function LandingPage() {
  const { user } = useAuth()

  return (
    <div className="min-h-screen flex flex-col bg-stone-50">
      <header className="border-b border-stone-200 bg-stone-50/90 backdrop-blur-sm sticky top-0 z-20">
        <div className="max-w-4xl mx-auto px-6 h-14 flex items-center justify-between">
          <Link to={user ? '/dashboard' : '/'} className="font-serif text-lg text-stone-900 tracking-tight">
            AI Grader
          </Link>
          <nav className="flex items-center gap-3">
            {user ? (
              <>
                <Link to="/dashboard" className="btn-ghost px-3.5 py-2">Dashboard</Link>
                <Link to="/app/upload" className="btn-primary px-3.5 py-2">Upload</Link>
              </>
            ) : (
              <>
                <Link to="/login" className="btn-ghost px-3.5 py-2">Sign in</Link>
                <Link to="/register" className="btn-primary px-3.5 py-2">Get started</Link>
              </>
            )}
          </nav>
        </div>
      </header>

      <main className="flex-1">
        <section className="max-w-4xl mx-auto px-6 pt-16 pb-14 md:pt-20 md:pb-16 animate-fade-in">
          <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full border border-stone-200 bg-white text-xs text-stone-500 tracking-wide uppercase">
            <span className="w-1.5 h-1.5 rounded-full bg-accent" aria-hidden="true" />
            Automated paper evaluation
          </div>

          <h1 className="mt-5 font-serif text-4xl md:text-5xl leading-tight text-stone-900 max-w-3xl">
            Fast, reliable grading for handwritten and typed answer sheets
          </h1>

          <p className="mt-5 text-base text-stone-600 leading-relaxed max-w-2xl">
            Upload a PDF, apply your rubric, and receive structured feedback with scores and comments per criterion.
            Rules like “best 2 of 3” are supported directly from the answer scheme.
          </p>

          <div className="mt-8 flex flex-wrap items-center gap-3">
            <Link to={user ? '/app/upload' : '/register'} className="btn-primary px-5 py-2.5">
              {user ? 'Upload paper' : 'Create account'}
            </Link>
            <Link to={user ? '/dashboard' : '/login'} className="btn-ghost px-5 py-2.5">
              {user ? 'View results' : 'Sign in'}
            </Link>
          </div>

          <div className="mt-8 grid sm:grid-cols-2 gap-2.5 text-sm text-stone-500">
            <p className="inline-flex items-center gap-2"><span className="text-accent"><CheckIcon /></span> Per-question score breakdown</p>
            <p className="inline-flex items-center gap-2"><span className="text-accent"><CheckIcon /></span> Dynamic rubric rules (best-of logic)</p>
            <p className="inline-flex items-center gap-2"><span className="text-accent"><CheckIcon /></span> Asynchronous processing pipeline</p>
            <p className="inline-flex items-center gap-2"><span className="text-accent"><CheckIcon /></span> Secure JWT authentication</p>
          </div>
        </section>

        <section className="max-w-4xl mx-auto px-6 pb-14 md:pb-16">
          <div className="card px-6 py-6 md:px-7 md:py-7 bg-white">
            <div className="flex items-center justify-between gap-4 flex-wrap">
              <div>
                <p className="label mb-1">Sample Output</p>
                <h2 className="font-serif text-2xl text-stone-900">Midterm Evaluation</h2>
              </div>
              <div className="text-right">
                <p className="text-xs uppercase tracking-wide text-stone-400">Final score</p>
                <p className="font-serif text-3xl text-stone-900">38 / 40</p>
              </div>
            </div>

            <div className="mt-5 space-y-3">
              {[
                { name: 'Q1 — Thermodynamics', score: 18, max: 20, dropped: false },
                { name: 'Q2 — Fluid Mechanics', score: 20, max: 20, dropped: false },
                { name: 'Q3 — Heat Transfer', score: 14, max: 20, dropped: true },
              ].map((item) => (
                <div key={item.name} className="rounded border border-stone-200 px-3.5 py-3 bg-stone-50">
                  <div className="flex items-center justify-between gap-3">
                    <p className={`text-sm ${item.dropped ? 'text-stone-400 line-through' : 'text-stone-700'}`}>
                      {item.name}
                      {item.dropped && <span className="ml-2 text-[11px] text-stone-400 uppercase tracking-wide">dropped (best 2 rule)</span>}
                    </p>
                    <p className={`text-xs font-mono ${item.dropped ? 'text-stone-400' : 'text-stone-600'}`}>
                      {item.score}/{item.max}
                    </p>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </section>

        <section className="max-w-4xl mx-auto px-6 pb-20">
          <div className="grid md:grid-cols-3 gap-3">
            <Feature
              icon={<BoltIcon />}
              title="Low-latency pipeline"
              text="Concurrent rendering and optimized payloads reduce grading time for multi-page submissions."
            />
            <Feature
              icon={<RuleIcon />}
              title="Rubric-aware scoring"
              text="The grader reads and applies paper-specific rules from your answer scheme before finalizing marks."
            />
            <Feature
              icon={<BarsIcon />}
              title="Clear result reports"
              text="Each result includes summary, criterion-level comments, and a score that fits your maximum marks."
            />
          </div>
        </section>
      </main>

      <footer className="border-t border-stone-100 py-5">
        <div className="max-w-4xl mx-auto px-6 flex items-center justify-between">
          <span className="text-xs text-stone-300">AI Grader © 2026</span>
          <span className="text-xs text-stone-300">Automated paper evaluation</span>
        </div>
      </footer>
    </div>
  )
}

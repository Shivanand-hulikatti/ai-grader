import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import Layout from '../components/Layout'
import StatusBadge from '../components/StatusBadge'
import ScoreRing from '../components/ScoreRing'
import { resultsApi } from '../api/client'

function formatDate(iso) {
  return new Date(iso).toLocaleDateString('en-GB', {
    day: '2-digit', month: 'short', year: 'numeric',
  })
}

export default function DashboardPage() {
  const [results, setResults] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  async function fetchResults() {
    setLoading(true)
    setError('')
    try {
      const data = await resultsApi.list()
      setResults(data.results ?? [])
    } catch (err) {
      setError(err.message || 'Failed to load results')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchResults() }, [])

  return (
    <Layout>
      {/* Header row */}
      <div className="flex items-end justify-between mb-8">
        <div>
          <h2 className="font-serif text-2xl text-stone-900">Submissions</h2>
          <p className="mt-1 text-sm text-stone-400">Your graded and pending papers</p>
        </div>
        <Link to="/app/upload" className="btn-primary">
          <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M4 16v1a2 2 0 002 2h12a2 2 0 002-2v-1M12 12V4m0 0l-3 3m3-3l3 3" />
          </svg>
          Upload paper
        </Link>
      </div>

      {/* States */}
      {loading && (
        <div className="space-y-3">
          {[...Array(4)].map((_, i) => (
            <div key={i} className="card h-20 animate-pulse bg-stone-100" />
          ))}
        </div>
      )}

      {error && !loading && (
        <div className="card px-5 py-4 text-sm text-red-500 border-red-100 bg-red-50">
          {error}
          <button onClick={fetchResults} className="ml-3 underline text-stone-700">Retry</button>
        </div>
      )}

      {!loading && !error && results.length === 0 && (
        <div className="card px-8 py-16 text-center">
          <svg className="mx-auto mb-4 w-10 h-10 text-stone-300" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 12h6m-3-3v6M4 6h16M4 10h16M4 14h8" />
          </svg>
          <p className="text-stone-400 text-sm">No submissions yet.</p>
          <Link to="/app/upload" className="btn-ghost mt-4 inline-flex">Upload your first paper</Link>
        </div>
      )}

      {!loading && !error && results.length > 0 && (
        <div className="space-y-2">
          {results.map(({ submission, grade }) => (
            <Link
              key={submission.id}
              to={`/app/results/${submission.id}`}
              className="card flex items-center gap-5 px-5 py-4 hover:bg-stone-50 transition-colors group"
            >
              {/* Score */}
              <div className="shrink-0">
                {grade ? (
                  <ScoreRing score={grade.score} max={submission.max_score || 100} size={52} />
                ) : (
                  <div className="w-[52px] h-[52px] rounded-full border-[5px] border-stone-200 flex items-center justify-center">
                    <span className="text-xs text-stone-300">—</span>
                  </div>
                )}
              </div>

              {/* Meta */}
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2.5 flex-wrap">
                  <span className="font-medium text-stone-800 text-sm truncate">
                    {submission.course || 'Submission'}
                  </span>
                  {submission.roll_no && (
                    <span className="font-mono text-xs text-stone-400">#{submission.roll_no}</span>
                  )}
                </div>
                <p className="text-xs text-stone-400 mt-0.5">{formatDate(submission.created_at)}</p>
              </div>

              {/* Status */}
              <div className="shrink-0 flex items-center gap-3">
                <StatusBadge status={submission.status} />
                <svg
                  className="w-4 h-4 text-stone-300 group-hover:text-stone-500 transition-colors"
                  fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}
                >
                  <path strokeLinecap="round" strokeLinejoin="round" d="M9 5l7 7-7 7" />
                </svg>
              </div>
            </Link>
          ))}
        </div>
      )}
    </Layout>
  )
}

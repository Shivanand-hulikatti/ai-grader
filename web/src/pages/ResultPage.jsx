import { useState, useEffect, useRef } from 'react'
import { useParams, Link } from 'react-router-dom'
import Layout from '../components/Layout'
import StatusBadge from '../components/StatusBadge'
import ScoreRing from '../components/ScoreRing'
import { resultsApi } from '../api/client'

function formatDate(iso) {
  return new Date(iso).toLocaleString('en-GB', {
    day: '2-digit', month: 'short', year: 'numeric',
    hour: '2-digit', minute: '2-digit',
  })
}

function parseFeedback(raw) {
  if (!raw) return null
  try { return typeof raw === 'string' ? JSON.parse(raw) : raw } catch (_) { return null }
}

export default function ResultPage() {
  const { id } = useParams()
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const pollRef = useRef(null)

  async function load() {
    try {
      const res = await resultsApi.get(id)
      setData(res)
      // Stop polling once graded or failed
      if (res.submission?.status === 'graded' || res.submission?.status === 'failed') {
        clearInterval(pollRef.current)
      }
    } catch (err) {
      setError(err.message || 'Failed to load result')
      clearInterval(pollRef.current)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // Poll every 5 s while pending
    pollRef.current = setInterval(load, 5000)
    return () => clearInterval(pollRef.current)
  }, [id])

  const sub = data?.submission
  const grade = data?.grade
  const feedback = parseFeedback(grade?.feedback)

  return (
    <Layout>
      {/* Back */}
      <Link
        to="/dashboard"
        className="inline-flex items-center gap-1.5 text-sm text-stone-400 hover:text-stone-700 transition-colors mb-7"
      >
        <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M15 19l-7-7 7-7" />
        </svg>
        All submissions
      </Link>

      {loading && (
        <div className="space-y-3">
          <div className="card h-28 animate-pulse bg-stone-100" />
          <div className="card h-48 animate-pulse bg-stone-100" />
        </div>
      )}

      {error && !loading && (
        <div className="card px-5 py-4 text-sm text-red-500 border-red-100 bg-red-50">{error}</div>
      )}

      {!loading && !error && sub && (
        <div className="space-y-4 animate-slide-up">
          {/* Overview card */}
          <div className="card px-6 py-5 flex items-center gap-6">
            <ScoreRing
              score={grade?.score ?? 0}
              max={sub.max_score || 100}
              size={72}
            />
            <div className="flex-1">
              <div className="flex items-center gap-3 flex-wrap">
                <h2 className="font-serif text-xl text-stone-900">
                  {sub.course || 'Submission'}
                </h2>
                {sub.roll_no && (
                  <span className="font-mono text-xs text-stone-400">#{sub.roll_no}</span>
                )}
                <StatusBadge status={sub.status} />
              </div>
              <p className="text-xs text-stone-400 mt-1">{formatDate(sub.created_at)}</p>
              {grade && (
                <p className="text-sm text-stone-600 mt-2">
                  Score: <span className="font-medium text-stone-900">{grade.score}</span>
                  <span className="text-stone-400"> / {sub.max_score || 100}</span>
                </p>
              )}
            </div>
          </div>

          {/* Pending state */}
          {sub.status !== 'graded' && sub.status !== 'failed' && (
            <div className="card px-6 py-8 text-center">
              <div className="inline-flex items-center gap-2 text-sm text-amber-600 bg-amber-50 border border-amber-100 rounded-full px-4 py-1.5 mb-3">
                <span className="w-2 h-2 rounded-full bg-amber-400 animate-pulse" />
                Grading in progress
              </div>
              <p className="text-xs text-stone-400">This page refreshes automatically every 5 seconds.</p>
            </div>
          )}

          {/* Error state */}
          {sub.status === 'failed' && (
            <div className="card px-6 py-5 border-red-100 bg-red-50">
              <p className="text-sm font-medium text-red-600">Grading failed</p>
              {sub.error_message && (
                <p className="text-xs text-red-400 mt-1">{sub.error_message}</p>
              )}
            </div>
          )}

          {/* Feedback */}
          {grade && feedback && (
            <div className="card px-6 py-5 space-y-5">
              <div>
                <p className="label">Summary</p>
                <p className="text-sm text-stone-700 leading-relaxed">{feedback.summary}</p>
              </div>

              {feedback.criteria && feedback.criteria.length > 0 && (
                <div>
                  <p className="label mb-3">Criteria breakdown</p>
                  <div className="space-y-3">
                    {feedback.criteria.map((c, i) => (
                      <div key={i} className="flex items-start gap-4">
                        {/* Mini bar */}
                        <div className="shrink-0 w-28 pt-1">
                          <div className="h-1.5 rounded-full bg-stone-100 overflow-hidden">
                            <div
                              className="h-full rounded-full bg-stone-700 transition-all duration-700"
                              style={{ width: `${Math.min((c.score / (sub.max_score || 100)) * 100 * 3, 100)}%` }}
                            />
                          </div>
                          <span className="text-xs text-stone-400 mt-1 block">{c.score} pts</span>
                        </div>
                        <div className="flex-1">
                          <p className="text-sm font-medium text-stone-800">{c.name}</p>
                          <p className="text-xs text-stone-500 mt-0.5 leading-relaxed">{c.comment}</p>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}

          {/* Raw feedback fallback */}
          {grade && !feedback && (
            <div className="card px-6 py-5">
              <p className="label">Feedback</p>
              <pre className="text-xs text-stone-600 font-mono whitespace-pre-wrap leading-relaxed mt-2">
                {grade.feedback}
              </pre>
            </div>
          )}

          {/* Submission meta */}
          <div className="card px-6 py-4">
            <p className="label mb-3">Details</p>
            <dl className="grid grid-cols-2 gap-y-2 text-sm">
              <dt className="text-stone-400">Submission ID</dt>
              <dd className="font-mono text-xs text-stone-600 truncate">{sub.id}</dd>
              <dt className="text-stone-400">File size</dt>
              <dd className="text-stone-700">{sub.file_size ? `${(sub.file_size / 1024).toFixed(1)} KB` : '—'}</dd>
              <dt className="text-stone-400">Max score</dt>
              <dd className="text-stone-700">{sub.max_score}</dd>
            </dl>
          </div>
        </div>
      )}
    </Layout>
  )
}

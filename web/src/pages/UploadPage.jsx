import { useState, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import Layout from '../components/Layout'
import { uploadApi } from '../api/client'

export default function UploadPage() {
  const navigate = useNavigate()
  const fileRef = useRef(null)
  const [dragging, setDragging] = useState(false)
  const [file, setFile] = useState(null)
  const [form, setForm] = useState({ roll_no: '', course: '', max_score: '100', answer_scheme: '' })
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  function set(field) {
    return (e) => setForm((f) => ({ ...f, [field]: e.target.value }))
  }

  function pickFile(f) {
    if (f && f.type === 'application/pdf') setFile(f)
    else setError('Only PDF files are accepted')
  }

  function onDrop(e) {
    e.preventDefault()
    setDragging(false)
    const f = e.dataTransfer.files?.[0]
    pickFile(f)
  }

  async function handleSubmit(e) {
    e.preventDefault()
    if (!file) { setError('Please select a PDF file'); return }
    setError('')
    setLoading(true)
    try {
      const fd = new FormData()
      fd.append('file', file)
      if (form.roll_no) fd.append('roll_no', form.roll_no)
      if (form.course) fd.append('course', form.course)
      fd.append('max_score', form.max_score)
      if (form.answer_scheme) fd.append('answer_scheme', form.answer_scheme)
      const data = await uploadApi.upload(fd)
      navigate(`/app/results/${data.submission?.id ?? ''}`)
    } catch (err) {
      setError(err.message || 'Upload failed')
    } finally {
      setLoading(false)
    }
  }

  const dropClasses = [
    'rounded-md border-2 border-dashed transition-all duration-200 cursor-pointer flex flex-col items-center justify-center gap-2 py-12 px-6 text-center',
    dragging ? 'border-stone-400 bg-stone-100' : file ? 'border-stone-300 bg-stone-50' : 'border-stone-200 hover:border-stone-300 hover:bg-stone-50',
  ].join(' ')

  return (
    <Layout>
      <div className="max-w-xl">
        <div className="mb-8">
          <h2 className="font-serif text-2xl text-stone-900">Upload paper</h2>
          <p className="mt-1 text-sm text-stone-400">Submit a PDF for AI grading</p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-5">
          {/* Drop zone */}
          <div
            className={dropClasses}
            onDragEnter={() => setDragging(true)}
            onDragOver={(e) => { e.preventDefault(); setDragging(true) }}
            onDragLeave={() => setDragging(false)}
            onDrop={onDrop}
            onClick={() => fileRef.current?.click()}
          >
            <input
              ref={fileRef}
              type="file"
              accept="application/pdf"
              className="hidden"
              onChange={(e) => pickFile(e.target.files?.[0])}
            />
            {file ? (
              <>
                <svg className="w-8 h-8 text-stone-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                <p className="text-sm text-stone-700 font-medium">{file.name}</p>
                <p className="text-xs text-stone-400">{(file.size / 1024).toFixed(0)} KB — click to change</p>
              </>
            ) : (
              <>
                <svg className="w-8 h-8 text-stone-300" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M4 16v1a2 2 0 002 2h12a2 2 0 002-2v-1M12 12V4m0 0l-3 3m3-3l3 3" />
                </svg>
                <p className="text-sm text-stone-500">Drop a PDF here or <span className="underline underline-offset-2 text-stone-700">browse</span></p>
                <p className="text-xs text-stone-300">PDF files only</p>
              </>
            )}
          </div>

          {/* Fields */}
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="label">Roll number</label>
              <input
                type="text"
                placeholder="e.g. 21CS042"
                value={form.roll_no}
                onChange={set('roll_no')}
                className="input-field"
              />
            </div>
            <div>
              <label className="label">Course</label>
              <input
                type="text"
                placeholder="e.g. Mathematics"
                value={form.course}
                onChange={set('course')}
                className="input-field"
              />
            </div>
          </div>

          <div>
            <label className="label">Max score</label>
            <input
              type="number"
              min="1"
              max="1000"
              value={form.max_score}
              onChange={set('max_score')}
              className="input-field"
            />
          </div>

          <div>
            <label className="label">
              Answer scheme <span className="normal-case text-stone-300 font-normal">(optional)</span>
            </label>
            <textarea
              rows={5}
              placeholder="Paste the expected answers or grading rubric…"
              value={form.answer_scheme}
              onChange={set('answer_scheme')}
              className="input-field resize-none font-mono text-xs leading-relaxed"
            />
          </div>

          {error && (
            <p className="text-xs text-red-500 bg-red-50 border border-red-100 rounded px-3 py-2">
              {error}
            </p>
          )}

          <div className="flex items-center gap-3 pt-1">
            <button type="submit" disabled={loading} className="btn-primary">
              {loading ? 'Uploading…' : 'Submit for grading'}
            </button>
            <button
              type="button"
              onClick={() => navigate('/dashboard')}
              className="btn-ghost"
            >
              Cancel
            </button>
          </div>
        </form>
      </div>
    </Layout>
  )
}

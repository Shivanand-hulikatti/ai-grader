const STATUS = {
  uploaded: { label: 'Uploaded', classes: 'bg-stone-100 text-stone-500' },
  processing: { label: 'Processing', classes: 'bg-amber-50 text-amber-600' },
  graded: { label: 'Graded', classes: 'bg-emerald-50 text-emerald-600' },
  failed: { label: 'Failed', classes: 'bg-red-50 text-red-500' },
}

export default function StatusBadge({ status }) {
  const cfg = STATUS[status] ?? { label: status, classes: 'bg-stone-100 text-stone-500' }
  return (
    <span
      className={`inline-flex items-center gap-1.5 px-2.5 py-1 text-xs font-medium rounded-full ${cfg.classes}`}
    >
      {status === 'processing' && (
        <span className="inline-block w-1.5 h-1.5 rounded-full bg-amber-400 animate-pulse" />
      )}
      {cfg.label}
    </span>
  )
}

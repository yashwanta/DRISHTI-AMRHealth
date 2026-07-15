// Client-side export helpers. Zero dependencies: CSV via Blob, PDF via the
// browser's print-to-PDF (window.print) with a print stylesheet, so we don't
// add jsPDF/file-saver to the bundle. Mirrors the Blob+anchor pattern already
// used in RdsLogsPage.tsx.

// downloadBlob triggers a file download for an in-memory Blob.
export function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

// csvField quotes a CSV cell when it contains a comma, quote, or newline.
function csvField(v: unknown): string {
  const s = v == null ? '' : String(v)
  if (/[",\n]/.test(s)) {
    return `"${s.replace(/"/g, '""')}"`
  }
  return s
}

// toCSV builds a CSV string from a list of objects + ordered column keys.
export function toCSV<T extends Record<string, unknown>>(
  rows: T[],
  columns: { key: keyof T; header: string }[],
): string {
  const head = columns.map(c => csvField(c.header)).join(',')
  const body = rows.map(row =>
    columns.map(c => csvField(row[c.key])).join(','),
  )
  return [head, ...body].join('\n')
}

// exportCSV builds and downloads a CSV from rows + columns.
export function exportCSV<T extends Record<string, unknown>>(
  rows: T[],
  columns: { key: keyof T; header: string }[],
  filename: string,
) {
  const csv = toCSV(rows, columns)
  // Prepend a BOM so Excel opens UTF-8 correctly.
  const blob = new Blob(['﻿' + csv], { type: 'text/csv;charset=utf-8' })
  downloadBlob(blob, filename)
}

// exportPrintPDF opens a print dialog over a self-contained printable document
// holding the given HTML body + title. The user picks "Save as PDF" as the
// destination. This avoids bundling a PDF library and produces crisp vector text.
export function exportPrintPDF(title: string, bodyHTML: string) {
  const win = window.open('', '_blank', 'width=900,height=700')
  if (!win) {
    alert('Pop-up blocked — allow pop-ups for this site to export PDF.')
    return
  }
  win.document.write(`<!doctype html>
<html><head><meta charset="utf-8"><title>${escapeHTML(title)}</title>
<style>
  :root { color-scheme: light; }
  body { font-family: -apple-system, Segoe UI, Roboto, sans-serif; color: #111; margin: 32px; font-size: 13px; line-height: 1.5; }
  h1 { font-size: 18px; margin: 0 0 4px; }
  .meta { color: #666; font-size: 12px; margin-bottom: 16px; }
  table { width: 100%; border-collapse: collapse; margin: 12px 0; font-size: 12px; }
  th, td { text-align: left; padding: 6px 8px; border-bottom: 1px solid #ddd; vertical-align: top; }
  th { background: #f3f4f6; font-weight: 600; }
  .badge { display: inline-block; padding: 1px 6px; border-radius: 999px; font-size: 10px; font-weight: 600; }
  .sev-high { background: #fee2e2; color: #991b1b; }
  .sev-med { background: #fef3c7; color: #92400e; }
  .sev-low { background: #f3f4f6; color: #555; }
  .summary { background: #eef2ff; border: 1px solid #c7d2fe; border-radius: 8px; padding: 12px 14px; margin: 12px 0; }
  .loc { color: #b45309; font-weight: 600; }
  @media print { body { margin: 12mm; } }
</style></head>
<body>${bodyHTML}
<script>window.onload = () => setTimeout(() => window.print(), 250)</script>
</body></html>`)
  win.document.close()
}

function escapeHTML(s: string): string {
  return s.replace(/[&<>"']/g, c => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c] as string))
}

// esc escapes a string for safe inclusion in the printable HTML.
export const esc = escapeHTML

// humanDuration renders seconds compactly (mirrors the backend humanDuration).
export function humanDuration(secs: number): string {
  if (!secs || secs <= 0) return '0 s'
  if (secs < 90) return `${secs} s`
  if (secs < 3600) return `~${Math.round(secs / 60)} min`
  const h = Math.floor(secs / 3600)
  const m = Math.round((secs % 3600) / 60)
  return m > 0 ? `~${h} h ${m} m` : `~${h} h`
}

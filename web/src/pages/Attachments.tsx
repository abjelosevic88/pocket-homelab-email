// Every file that came by mail, newest first: photos and documents, open the message or save to a Pocket app.
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { APP_NAME, attURL, fileIcon, loadApps, mailDate, who } from '../lib/mail'
import { fmtBytes } from '../lib/format'
import type { AppLink, AppsStatus, AttachmentRow } from '../lib/types'
import { Icon } from '../components/icons'
import { Empty, Menu, type MenuItem, Segmented, Spinner, rectOf, useDebounce, useToast, type Anchor } from '../components/ui'
import { PreviewModal } from '../components/dialogs'

type Kind = '' | 'images' | 'documents'

export default function Attachments() {
  const navigate = useNavigate()
  const { toast } = useToast()
  const [kind, setKind] = useState<Kind>('')
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 250)
  const [rows, setRows] = useState<AttachmentRow[] | null>(null)
  const [more, setMore] = useState(false)
  const [busy, setBusy] = useState(false)
  const [apps, setApps] = useState<AppsStatus | null>(null)
  const [menu, setMenu] = useState<{ a: AttachmentRow; at: Anchor } | null>(null)
  const [preview, setPreview] = useState<AttachmentRow | null>(null)
  const seq = useRef(0)
  useEffect(() => { loadApps().then(setApps) }, [])
  useEffect(() => {
    const n = ++seq.current
    setBusy(true)
    api.get<{ attachments: AttachmentRow[]; more: boolean }>(`${V1}/attachments?kind=${kind}&q=${encodeURIComponent(dq)}`)
      .then(r => { if (n === seq.current) { setRows(r.attachments || []); setMore(r.more) } })
      .catch(e => toast((e as Error).message, 'err')).finally(() => { if (n === seq.current) setBusy(false) })
  }, [kind, dq, toast])
  const loadMore = async () => {
    if (!rows || busy) return
    setBusy(true)
    try { const r = await api.get<{ attachments: AttachmentRow[]; more: boolean }>(`${V1}/attachments?kind=${kind}&q=${encodeURIComponent(dq)}&offset=${rows.length}`); setRows([...rows, ...r.attachments]); setMore(r.more) } catch { /* retry by scrolling */ } finally { setBusy(false) }
  }
  const save = async (a: AttachmentRow, app: 'documents' | 'files' | 'photos') => {
    try {
      const r = await api.post<{ links?: AppLink[]; message?: string }>(`${V1}/messages/${a.message_id}/save`, { app, attachment_ids: [a.id] })
      const url = r.links?.[0]?.item_url
      toast(r.message || `Saved to ${APP_NAME[app]}`, 'ok', url ? { label: 'Open', run: () => window.open(url, '_blank', 'noopener') } : undefined)
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const items = (a: AttachmentRow): MenuItem[] => {
    const ok = (s: 'documents' | 'files' | 'photos') => !!apps?.apps[s]?.configured
    const it = (s: 'documents' | 'files' | 'photos', label: string, icon: string) => ({ label, icon, disabled: !ok(s), hint: ok(s) ? undefined : 'not set up', onSelect: () => save(a, s) })
    return [
      { label: 'Open the message', icon: 'mail', onSelect: () => navigate(`/message/${a.message_id}`) },
      { label: 'Download', icon: 'download', onSelect: () => window.open(attURL(a.id, true)) },
      'sep', { heading: 'Save to' },
      it('documents', 'Pocket Documents', 'file-text'), it('files', 'Pocket Files', 'folder'),
      ...(a.mime.startsWith('image/') ? [it('photos', 'Photos', 'image')] : []),
    ]
  }
  return (
    <div className="page-pad">
      <div className="page-header">
        <div><h1>Attachments</h1><p className="sub">Every file that came with your mail. Click one to look at it.</p></div>
        <div className="actions">
          <Segmented size="sm" value={kind} onChange={setKind} options={[{ value: '' as Kind, label: 'All' }, { value: 'images' as Kind, label: 'Photos' }, { value: 'documents' as Kind, label: 'Documents' }]} />
          <input className="page-search" value={q} onChange={e => setQ(e.target.value)} placeholder="Name, subject or sender" aria-label="Find a file" />
        </div>
      </div>
      {rows === null ? <div className="loading"><Spinner /> Loading…</div> : rows.length === 0 ? <Empty title="No attachments" icon="paperclip">{q ? 'Nothing matches.' : 'Files people send you show up here.'}</Empty> : (
        <div className="att-gallery">
          {rows.map(a => {
            const img = a.mime.startsWith('image/')
            return (
              <div key={a.id} className="gal-item">
                <button type="button" className="gal-thumb" onClick={() => img || a.mime === 'application/pdf' ? setPreview(a) : navigate(`/message/${a.message_id}`)} title={a.filename}>
                  {img ? <span className="gal-img" style={{ backgroundImage: `url(${attURL(a.id)})` }} /> : <span className="gal-ico"><Icon name={fileIcon(a.mime, a.filename)} size={28} /><span className="gal-ext">{(a.filename.split('.').pop() || '').slice(0, 5).toUpperCase()}</span></span>}
                </button>
                <div className="gal-info">
                  <div className="gal-name ellipsis" title={a.filename}>{a.filename || 'file'}</div>
                  <div className="gal-sub ellipsis muted xsmall">{who(a.from)} · {mailDate(a.date)} · {fmtBytes(a.size)}</div>
                </div>
                <button type="button" className="btn ghost icon-only sm gal-more" aria-label="More" onClick={e => setMenu({ a, at: rectOf(e.currentTarget) })}><Icon name="more" size={16} /></button>
              </div>
            )
          })}
        </div>
      )}
      {more && <div className="list-more"><button type="button" className="btn" disabled={busy} onClick={loadMore}>{busy ? <Spinner size={12} /> : null}Show more</button></div>}
      {menu && <Menu anchor={menu.at} align="right" width={240} items={items(menu.a)} onClose={() => setMenu(null)} />}
      {preview && <PreviewModal a={preview} onClose={() => setPreview(null)} />}
    </div>
  )
}

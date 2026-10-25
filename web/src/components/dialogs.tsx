// Small dialogs: snooze, pick a time, move to a folder, preview a file, record an expense, make a task, attach from a Pocket app.
import { useEffect, useMemo, useState } from 'react'
import { api, V1 } from '../lib/api'
import { useMail } from '../lib/mail-context'
import { ROLE_ICON, ROLE_LABEL, attURL, at, fileIcon, localInput, snoozeChoices } from '../lib/mail'
import { fmtBytes, fmtDate } from '../lib/format'
import type { AIExtract, Attachment, DocumentHit, FileEntry, Message, MoneyAccount } from '../lib/types'
import { Icon } from './icons'
import { Select } from './Select'
import { Empty, Field, Loading, Menu, Modal, Spinner, useDebounce, useToast } from './ui'

const anchorOf = (r: DOMRect) => ({ x: r.left, y: r.top, w: r.width, h: r.height })

/** The snooze choices; onPick('') = "Pick date & time…". */
export function SnoozeMenu({ anchor, onClose, onPick }: { anchor: DOMRect; onClose: () => void; onPick: (until: string) => void }) {
  const items = snoozeChoices().map(c => ({ label: c.label, hint: c.hint, icon: 'alarm', onSelect: () => onPick(c.when.toISOString()) }))
  return <Menu anchor={anchorOf(anchor)} align="right" width={250} onClose={onClose} items={[{ heading: 'Snooze until' }, ...items, 'sep', { label: 'Pick date & time…', icon: 'calendar', onSelect: () => onPick('') }]} />
}

export function PickTimeDialog({ title, confirm = 'Snooze', onClose, onPick }: { title: string; confirm?: string; onClose: () => void; onPick: (iso: string) => void }) {
  const [v, setV] = useState(localInput(at(1, 8)))
  const t = new Date(v)
  const ok = !Number.isNaN(t.getTime()) && t.getTime() > Date.now()
  return (
    <Modal title={title} onClose={onClose} size="sm" footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" disabled={!ok} onClick={() => onPick(t.toISOString())}>{confirm}</button></>}>
      <Field label="Date and time"><input type="datetime-local" value={v} min={localInput(new Date())} onChange={e => setV(e.target.value)} autoFocus /></Field>
      {!ok && <p className="small bad mt-s">Pick a time in the future.</p>}
    </Modal>
  )
}

/** Move messages to a folder of their account. */
export function MoveDialog({ ids, messages, onClose, onMove }: { ids: number[]; messages: Message[]; onClose: () => void; onMove: (folderId: number) => void }) {
  const mail = useMail()
  const [q, setQ] = useState('')
  const acctIds = [...new Set(messages.map(m => m.account_id))]
  const one = acctIds.length <= 1
  const acct = mail.account(acctIds[0] || 0) || (mail.accounts.length === 1 ? mail.accounts[0] : undefined)
  const here = new Set(messages.map(m => m.folder_id))
  const folders = (acct?.folders || []).filter(f => f.selectable && !here.has(f.id) && (!q || (f.display || f.name).toLowerCase().includes(q.toLowerCase())))
  return (
    <Modal title={`Move ${ids.length > 1 ? `${ids.length} messages` : 'to a folder'}`} onClose={onClose} size="sm">
      {!one ? <p className="muted">These messages are in different accounts. Pick messages from one account to move them to one of its folders.</p> : <>
        <input value={q} onChange={e => setQ(e.target.value)} placeholder="Find a folder" autoFocus aria-label="Find a folder" className="mb" />
        <div className="pick-list">
          {folders.map(f => (
            <button key={f.id} type="button" className="pick-row" onClick={() => onMove(f.id)}>
              <Icon name={ROLE_ICON[f.role] || 'folder'} size={15} className="muted" />
              <span className="grow ellipsis">{(f.role && ROLE_LABEL[f.role]) || f.display || f.name}</span>
              {f.role === '' && f.name.includes(f.delimiter) && <span className="muted xsmall ellipsis">{f.name}</span>}
            </button>
          ))}
          {folders.length === 0 && <p className="muted small">No folder matches.</p>}
        </div>
      </>}
    </Modal>
  )
}

/** Look at an image or a PDF without downloading it. */
export function PreviewModal({ a, onClose }: { a: Attachment; onClose: () => void }) {
  const img = a.mime.startsWith('image/')
  const pdf = a.mime === 'application/pdf'
  return (
    <Modal title={a.filename || 'Attachment'} onClose={onClose} size="xl" className="preview-modal"
      footer={<><span className="muted small grow">{fmtBytes(a.size)}</span><a className="btn" href={attURL(a.id, true)}><Icon name="download" size={15} />Download</a></>}>
      <div className="preview-body">
        {img && <img src={attURL(a.id)} alt={a.filename} />}
        {pdf && <iframe title={a.filename} src={attURL(a.id)} />}
        {!img && !pdf && <Empty title="No preview for this file" icon={fileIcon(a.mime, a.filename)}>Download it to open it.</Empty>}
      </div>
    </Modal>
  )
}

/** Make a task from a message. */
export function TaskDialog({ m, suggest, onClose, onDone }: { m: Message; suggest?: { title: string; due?: string }; onClose: () => void; onDone: (url: string) => void }) {
  const { toast } = useToast()
  const [title, setTitle] = useState(suggest?.title || m.subject || '')
  const [due, setDue] = useState(suggest?.due?.slice(0, 10) || '')
  const [notes, setNotes] = useState('')
  const [busy, setBusy] = useState(false)
  const save = async () => {
    setBusy(true)
    try { const r = await api.post<{ link: { item_url: string } }>(`${V1}/messages/${m.id}/task`, { title, due: due || undefined, notes: notes || undefined }); onDone(r.link?.item_url || '') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Make a task" onClose={onClose} size="sm" footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" disabled={busy || !title.trim()} onClick={save}>{busy ? 'Adding…' : 'Add to Pocket Tasks'}</button></>}>
      <div className="stack">
        <Field label="Task"><input value={title} onChange={e => setTitle(e.target.value)} autoFocus /></Field>
        <Field label="Due (optional)"><input type="date" value={due} onChange={e => setDue(e.target.value)} /></Field>
        <Field label="Note (optional)" help="The task links back to this email."><textarea rows={3} value={notes} onChange={e => setNotes(e.target.value)} /></Field>
      </div>
    </Modal>
  )
}

/** Record a receipt as an expense in Pocket Money (prefilled by AI when it is on). */
export function ExpenseDialog({ m, aiReady, onClose, onDone }: { m: Message; aiReady: boolean; onClose: () => void; onDone: (url: string) => void }) {
  const { toast } = useToast()
  const [accounts, setAccounts] = useState<MoneyAccount[] | null>(null)
  const [f, setF] = useState({ amount: '', currency: 'EUR', date: m.date.slice(0, 10), description: m.from.name || m.subject, account_id: '' })
  const [reading, setReading] = useState(aiReady)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    api.get<{ accounts: MoneyAccount[] }>(`${V1}/apps/money/accounts`).then(r => {
      setAccounts(r.accounts || [])
      if (r.accounts?.[0]) setF(x => ({ ...x, account_id: x.account_id || String(r.accounts[0].id), currency: r.accounts[0].currency || x.currency }))
    }).catch(() => setAccounts([]))
    if (aiReady) api.post<AIExtract>(`${V1}/ai/extract`, { message_id: m.id }).then(r => {
      const x = r.expense
      if (x) setF(o => ({ ...o, amount: x.amount ? String(x.amount) : o.amount, currency: x.currency || o.currency, date: x.date?.slice(0, 10) || o.date, description: x.merchant || o.description }))
    }).catch(() => {}).finally(() => setReading(false))
  }, [m.id, aiReady])
  const amount = Number(f.amount.replace(',', '.'))
  const save = async () => {
    setBusy(true)
    try {
      const r = await api.post<{ link: { item_url: string } }>(`${V1}/messages/${m.id}/expense`, { amount, currency: f.currency, date: f.date, description: f.description, account_id: f.account_id ? Number(f.account_id) : undefined })
      onDone(r.link?.item_url || '')
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Record an expense" onClose={onClose} size="sm" footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" disabled={busy || !(amount > 0)} onClick={save}>{busy ? 'Saving…' : 'Add to Pocket Money'}</button></>}>
      <div className="stack">
        {reading && <p className="muted small row"><Spinner size={12} />Reading the receipt…</p>}
        <div className="two-col tight">
          <Field label="Amount"><input inputMode="decimal" value={f.amount} onChange={e => setF({ ...f, amount: e.target.value })} placeholder="0.00" autoFocus /></Field>
          <Field label="Currency"><input value={f.currency} onChange={e => setF({ ...f, currency: e.target.value.toUpperCase().slice(0, 3) })} /></Field>
        </div>
        <Field label="What"><input value={f.description} onChange={e => setF({ ...f, description: e.target.value })} /></Field>
        <div className="two-col tight">
          <Field label="Date"><input type="date" value={f.date} onChange={e => setF({ ...f, date: e.target.value })} /></Field>
          <Field label="Paid from">{accounts === null ? <Loading /> : <Select ariaLabel="Account" value={f.account_id} onChange={v => setF({ ...f, account_id: v })} options={accounts.map(a => ({ value: String(a.id), label: a.name, hint: a.currency }))} placeholder="Default account" />}</Field>
        </div>
      </div>
    </Modal>
  )
}

/** Pick a file from Pocket Files or a document from Pocket Documents to attach. */
export function AttachPicker({ draftId, onClose, onAdded }: { draftId: number; onClose: () => void; onAdded: () => void }) {
  const { toast } = useToast()
  const [src, setSrc] = useState<'files' | 'documents'>('files')
  const [path, setPath] = useState('/')
  const [entries, setEntries] = useState<FileEntry[] | null>(null)
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 250)
  const [docs, setDocs] = useState<DocumentHit[] | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState('')
  useEffect(() => {
    if (src !== 'files') return
    setEntries(null); setErr('')
    api.get<{ entries: FileEntry[] }>(`${V1}/apps/files/browse?path=${encodeURIComponent(path)}`).then(r => setEntries(r.entries || [])).catch(e => setErr((e as Error).message))
  }, [src, path])
  useEffect(() => {
    if (src !== 'documents') return
    setDocs(null); setErr('')
    api.get<{ documents: DocumentHit[] }>(`${V1}/apps/documents/search?q=${encodeURIComponent(dq)}`).then(r => setDocs(r.documents || [])).catch(e => setErr((e as Error).message))
  }, [src, dq])
  const add = async (ref: string) => {
    setBusy(ref)
    try { await api.post(`${V1}/drafts/${draftId}/files/from`, { app: src, ref }); toast('Attached'); onAdded() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const up = path.replace(/\/[^/]+\/?$/, '') || '/'
  const crumbs = useMemo(() => path.split('/').filter(Boolean), [path])
  return (
    <Modal title="Attach from your Pocket apps" onClose={onClose}>
      <div className="row mb">
        <button type="button" className={`chip day-chip ${src === 'files' ? 'on' : ''}`} onClick={() => setSrc('files')}><Icon name="folder" size={13} /> Files</button>
        <button type="button" className={`chip day-chip ${src === 'documents' ? 'on' : ''}`} onClick={() => setSrc('documents')}><Icon name="file-text" size={13} /> Documents</button>
      </div>
      {err && <div className="callout info"><Icon name="info" size={15} /><span>{err}</span></div>}
      {src === 'files' && !err && <>
        <div className="crumbs small mb">
          <button type="button" className="link-btn" onClick={() => setPath('/')}>Files</button>
          {crumbs.map((c, i) => <span key={i}> / <button type="button" className="link-btn" onClick={() => setPath('/' + crumbs.slice(0, i + 1).join('/'))}>{c}</button></span>)}
        </div>
        {entries === null ? <Loading /> : <div className="pick-list tall">
          {path !== '/' && <button type="button" className="pick-row" onClick={() => setPath(up)}><Icon name="arrow-up" size={15} className="muted" /><span className="grow">Up</span></button>}
          {entries.map(e => (
            <button key={e.path} type="button" className="pick-row" disabled={!!busy} onClick={() => e.dir ? setPath(e.path) : add(e.path)}>
              <Icon name={e.dir ? 'folder' : fileIcon(e.mime, e.name)} size={15} className="muted" />
              <span className="grow ellipsis">{e.name}</span>
              {busy === e.path ? <Spinner size={12} /> : !e.dir && <span className="muted xsmall">{fmtBytes(e.size)}</span>}
            </button>
          ))}
          {entries.length === 0 && <p className="muted small">This folder is empty.</p>}
        </div>}
      </>}
      {src === 'documents' && !err && <>
        <input value={q} onChange={e => setQ(e.target.value)} placeholder="Find a document" autoFocus aria-label="Find a document" className="mb" />
        {docs === null ? <Loading /> : <div className="pick-list tall">
          {docs.map(d => (
            <button key={d.id} type="button" className="pick-row" disabled={!!busy} onClick={() => add(String(d.id))}>
              <Icon name={fileIcon(d.mime)} size={15} className="muted" />
              <span className="grow ellipsis">{d.title}</span>
              {busy === String(d.id) ? <Spinner size={12} /> : <span className="muted xsmall">{fmtDate(d.created)}</span>}
            </button>
          ))}
          {docs.length === 0 && <p className="muted small">Nothing found.</p>}
        </div>}
      </>}
    </Modal>
  )
}

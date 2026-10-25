// Add a date from an email to Pocket Calendar (prefilled by AI when it is on).
import { useEffect, useState } from 'react'
import { api, V1 } from '../lib/api'
import { localInput } from '../lib/mail'
import type { AIExtract, AppLink, Message } from '../lib/types'
import { Field, Modal, Spinner, Switch, useToast } from './ui'

export function EventDialog({ m, aiReady, onClose, onDone }: { m: Message; aiReady: boolean; onClose: () => void; onDone: (url: string) => void }) {
  const { toast } = useToast()
  const start0 = new Date(); start0.setDate(start0.getDate() + 1); start0.setHours(10, 0, 0, 0)
  const [f, setF] = useState({ title: m.subject, start: localInput(start0), end: localInput(new Date(start0.getTime() + 3600000)), all_day: false, location: '' })
  const [reading, setReading] = useState(aiReady)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!aiReady) return
    api.post<AIExtract>(`${V1}/ai/extract`, { message_id: m.id }).then(r => {
      const e = r.events?.[0]
      if (!e) return
      const s = new Date(e.start)
      if (Number.isNaN(s.getTime())) return
      const en = e.end ? new Date(e.end) : new Date(s.getTime() + 3600000)
      setF({ title: e.title || m.subject, start: localInput(s), end: localInput(en), all_day: !!e.all_day, location: e.location || '' })
    }).catch(() => {}).finally(() => setReading(false))
  }, [m.id, m.subject, aiReady])
  const save = async () => {
    setBusy(true)
    try {
      const body = { title: f.title, start: new Date(f.start).toISOString(), end: new Date(f.end).toISOString(), all_day: f.all_day, location: f.location }
      const r = await api.post<{ link?: AppLink }>(`${V1}/messages/${m.id}/event`, body)
      onDone(r.link?.item_url || '')
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Add to calendar" onClose={onClose} size="sm" footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" disabled={busy || !f.title.trim()} onClick={save}>{busy ? 'Adding…' : 'Add to Pocket Calendar'}</button></>}>
      <div className="stack">
        {reading && <p className="muted small row"><Spinner size={12} />Looking for the date in the email…</p>}
        <Field label="What"><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} autoFocus /></Field>
        <label className="check-row"><Switch size="sm" checked={f.all_day} onChange={v => setF({ ...f, all_day: v })} label="All day" />All day</label>
        <div className="two-col tight">
          <Field label="Starts"><input type="datetime-local" value={f.start} onChange={e => setF({ ...f, start: e.target.value })} /></Field>
          <Field label="Ends"><input type="datetime-local" value={f.end} onChange={e => setF({ ...f, end: e.target.value })} /></Field>
        </div>
        <Field label="Where (optional)"><input value={f.location} onChange={e => setF({ ...f, location: e.target.value })} /></Field>
      </div>
    </Modal>
  )
}

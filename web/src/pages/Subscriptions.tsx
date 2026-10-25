// Newsletters and mailing lists you get, by how much they send: unsubscribe in one click, archive all, or mute.
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { announceChange, mailDate } from '../lib/mail'
import type { ActionResult, Subscription, UnsubscribeResult } from '../lib/types'
import { Icon } from '../components/icons'
import { Avatar, Confirm, Empty, Segmented, Spinner, useAsync, useToast } from '../components/ui'

type Sort = 'count' | 'unread' | 'last'

export default function Subscriptions() {
  const navigate = useNavigate()
  const { toast } = useToast()
  const { data, reload, loading } = useAsync(() => api.get<{ subscriptions: Subscription[] }>(`${V1}/subscriptions`), [])
  const [sort, setSort] = useState<Sort>('count')
  const [busy, setBusy] = useState('')
  const [confirm, setConfirm] = useState<Subscription | null>(null)
  const list = [...(data?.subscriptions || [])].sort((a, b) => sort === 'count' ? b.count - a.count : sort === 'unread' ? b.unread - a.unread : b.last.localeCompare(a.last))
  const unsub = async (s: Subscription) => {
    setBusy(s.addr)
    try {
      const r = await api.post<UnsubscribeResult>(`${V1}/subscriptions/unsubscribe`, { addr: s.addr })
      if (r.method === 'url' && r.url) { window.open(r.url, '_blank', 'noopener'); toast('Their page opened in a new tab: finish there.') }
      else toast(r.message || `Unsubscribed from ${s.name || s.addr}`)
      reload()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const archiveAll = async (s: Subscription) => {
    setConfirm(null); setBusy(s.addr)
    try {
      const r = await api.post<ActionResult & { count?: number }>(`${V1}/subscriptions/archive`, { addr: s.addr })
      announceChange()
      toast(r.message || `Archived ${r.count ?? ''} messages`.trim(), 'ok', r.ops?.length ? { label: 'Undo', run: async () => { await api.post(`${V1}/undo`, { ops: r.ops }); announceChange(); reload() } } : undefined)
      reload()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const mute = async (s: Subscription) => {
    try { await api.put(`${V1}/senders/${encodeURIComponent(s.addr)}`, { muted: !s.muted }); toast(s.muted ? 'Unmuted' : 'Muted: no notifications from them'); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <div className="page-pad narrow">
      <div className="page-header">
        <div><h1>Subscriptions</h1><p className="sub">Newsletters and lists that write to you. Leave the ones you never read.</p></div>
        <div className="actions"><Segmented size="sm" value={sort} onChange={setSort} options={[{ value: 'count' as Sort, label: 'Most mail' }, { value: 'unread' as Sort, label: 'Most unread' }, { value: 'last' as Sort, label: 'Latest' }]} /></div>
      </div>
      {!data && loading ? <div className="loading"><Spinner /> Loading…</div> : list.length === 0 ? <Empty title="No subscriptions" icon="rss">Newsletters and mailing lists show up here.</Empty> : (
        <div className="card"><ul className="sub-list">
          {list.map(s => {
            const gone = !!s.unsubscribed_at
            const readRate = s.count ? Math.round((1 - s.unread / s.count) * 100) : 0
            return (
              <li key={s.addr} className={`sub-row ${gone ? 'gone' : ''}`}>
                <Avatar name={s.name} email={s.addr} size={36} />
                <div className="grow sub-main" onClick={() => navigate(`/search?q=${encodeURIComponent('from:' + s.addr)}`)} role="button" tabIndex={0}>
                  <div className="row"><b className="ellipsis">{s.name || s.addr}</b>{gone && <span className="badge">unsubscribed</span>}{s.muted && <span className="badge">muted</span>}</div>
                  <div className="muted small ellipsis">{s.last_subject}</div>
                  <div className="muted xsmall">{s.count} {s.count === 1 ? 'message' : 'messages'} · {s.unread ? `${s.unread} unread · ` : ''}you open {readRate}% · last {mailDate(s.last)}</div>
                </div>
                <div className="sub-acts">
                  {busy === s.addr ? <Spinner /> : <>
                    <button type="button" className="btn sm" disabled={gone || !s.can_unsubscribe} title={s.can_unsubscribe ? (s.one_click ? 'Leave the list in one click' : 'Leave the list') : 'This sender gives no way to unsubscribe'} onClick={() => unsub(s)}>{gone ? 'Left' : 'Unsubscribe'}</button>
                    <button type="button" className="btn ghost icon-only sm" aria-label="Archive all" title="Archive all their mail" onClick={() => setConfirm(s)}><Icon name="archive" size={15} /></button>
                    <button type="button" className="btn ghost icon-only sm" aria-label={s.muted ? 'Unmute' : 'Mute'} title={s.muted ? 'Unmute' : 'Mute notifications'} onClick={() => mute(s)}><Icon name={s.muted ? 'bell' : 'bell-off'} size={15} /></button>
                  </>}
                </div>
              </li>
            )
          })}
        </ul></div>
      )}
      {confirm && <Confirm title={`Archive all mail from ${confirm.name || confirm.addr}?`} message={`${confirm.count} messages leave the inbox. You can undo it right after.`} danger={false} confirmLabel="Archive all" onCancel={() => setConfirm(null)} onConfirm={() => archiveAll(confirm)} />}
    </div>
  )
}

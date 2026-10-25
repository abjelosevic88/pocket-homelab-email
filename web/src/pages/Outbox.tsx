// Drafts you started here, and what is waiting to go out: scheduled messages and the ones that could not be sent.
import { api, V1 } from '../lib/api'
import { announceChange, joinAddrs, mailDate, openCompose, useLive, useOnChange } from '../lib/mail'
import { fmtDateTime } from '../lib/format'
import type { Draft } from '../lib/types'
import { Icon } from '../components/icons'
import { Empty, Spinner, useAsync, useToast } from '../components/ui'
import { useMail } from '../lib/mail-context'

const plain = (html: string) => html.replace(/<blockquote[\s\S]*$/i, '').replace(/<[^>]+>/g, ' ').replace(/&nbsp;/g, ' ').replace(/\s+/g, ' ').trim()

export default function Outbox({ kind }: { kind: 'drafts' | 'scheduled' }) {
  const { toast } = useToast()
  const mail = useMail()
  const status = kind === 'drafts' ? 'draft' : 'queued,failed,sending'
  const { data, reload, loading } = useAsync(() => api.get<{ drafts: Draft[] }>(`${V1}/drafts?status=${status}`), [status])
  useOnChange(reload)
  useLive(e => { if (e.type === 'sent') reload() })
  const list = data?.drafts || []
  const cancel = async (d: Draft) => { try { await api.post(`${V1}/drafts/${d.id}/cancel`); announceChange(); toast('Back in your drafts'); reload() } catch (e) { toast((e as Error).message, 'err') } }
  const retry = async (d: Draft) => { try { await api.post(`${V1}/drafts/${d.id}/send`, {}); announceChange(); toast('Sending again…'); reload() } catch (e) { toast((e as Error).message, 'err') } }
  const del = async (d: Draft) => { try { await api.del(`${V1}/drafts/${d.id}`); announceChange(); toast('Draft deleted'); reload() } catch (e) { toast((e as Error).message, 'err') } }
  const failed = list.filter(d => d.status === 'failed')
  const waiting = list.filter(d => d.status !== 'failed')
  const row = (d: Draft) => {
    const acct = mail.account(d.account_id)
    return (
      <li key={d.id} className="out-row">
        <div className="grow out-main" role="button" tabIndex={0} onClick={() => d.status === 'draft' || d.status === 'failed' ? openCompose({ draft_id: d.id }) : undefined}>
          <div className="row"><b className="ellipsis">{d.to.length ? `To: ${joinAddrs(d.to)}` : 'No recipients yet'}</b>{d.source && <span className="badge">from {d.source}</span>}</div>
          <div className="ellipsis">{d.subject || <span className="muted">(no subject)</span>}</div>
          <div className="muted small ellipsis">{plain(d.body_html).slice(0, 140)}</div>
          {d.status === 'failed' && <div className="small bad">Not sent: {d.error || 'the server said no'}</div>}
        </div>
        <div className="out-side">
          <span className="muted xsmall">{d.status === 'queued' ? <><Icon name="clock" size={11} /> {fmtDateTime(d.send_at)}</> : d.status === 'sending' ? 'sending…' : mailDate(d.updated_at)}</span>
          {acct && mail.accounts.length > 1 && <span className="muted xsmall">{acct.label || acct.email}</span>}
          <div className="row">
            {d.status === 'queued' && <button type="button" className="btn sm" onClick={() => cancel(d)}>Don’t send</button>}
            {d.status === 'failed' && <><button type="button" className="btn sm primary" onClick={() => retry(d)}>Try again</button><button type="button" className="btn sm" onClick={() => openCompose({ draft_id: d.id })}>Edit</button></>}
            {d.status === 'draft' && <button type="button" className="btn sm" onClick={() => openCompose({ draft_id: d.id })}>Continue</button>}
            {d.status !== 'sending' && <button type="button" className="btn ghost icon-only sm" aria-label="Delete" title="Delete" onClick={() => del(d)}><Icon name="trash" size={14} /></button>}
          </div>
        </div>
      </li>
    )
  }
  return (
    <div className="page-pad narrow">
      <div className="page-header"><div>
        <h1>{kind === 'drafts' ? 'Drafts' : 'Scheduled'}</h1>
        <p className="sub">{kind === 'drafts' ? 'What you started writing here. It is saved as you type.' : 'Messages waiting for their time, and the ones that could not be sent.'}</p>
      </div></div>
      {!data && loading ? <div className="loading"><Spinner /> Loading…</div> : list.length === 0 ? <Empty title={kind === 'drafts' ? 'No drafts' : 'Nothing waiting'} icon={kind === 'drafts' ? 'file-text' : 'clock'}>{kind === 'drafts' ? 'Press c to write a new message.' : 'Use the arrow next to Send to send a message later.'}</Empty> : <>
        {failed.length > 0 && <><h4 className="group-h">Not sent</h4><div className="card mb"><ul className="out-list">{failed.map(row)}</ul></div></>}
        {waiting.length > 0 && <div className="card"><ul className="out-list">{waiting.map(row)}</ul></div>}
      </>}
    </div>
  )
}

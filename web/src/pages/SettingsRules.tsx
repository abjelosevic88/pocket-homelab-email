// Settings → Rules (what happens to new mail by itself) and the templates manager.
import { useEffect, useState } from 'react'
import { api, V1 } from '../lib/api'
import { CATEGORIES, loadApps } from '../lib/mail'
import type { AppsStatus, Category, Folder, Rule, Sidebar, Template } from '../lib/types'
import { Icon } from '../components/icons'
import { Confirm, CoreHint, Field, Loading, Modal, Switch, useAsync, useToast } from '../components/ui'
import { Select } from '../components/Select'
import { RichEditor } from '../components/RichEditor'
import { Section } from './settings-kit'

function describe(r: Rule): string {
  const m: string[] = []
  if (r.match.from) m.push(`from “${r.match.from}”`)
  if (r.match.to) m.push(`to “${r.match.to}”`)
  if (r.match.subject) m.push(`subject has “${r.match.subject}”`)
  if (r.match.has_attachment) m.push('with attachments')
  if (r.match.category) m.push(`in ${r.match.category}`)
  const a: string[] = []
  if (r.actions.mark_read) a.push('mark read')
  if (r.actions.star) a.push('star')
  if (r.actions.archive) a.push('archive')
  if (r.actions.move_to) a.push(`move to ${r.actions.move_to}`)
  if (r.actions.category) a.push(`put in ${r.actions.category}`)
  if (r.actions.save_to?.length) a.push(`save attachments to ${r.actions.save_to.join(' and ')}`)
  if (r.actions.task) a.push('make a task')
  if (r.actions.notify) a.push('notify me')
  return `Mail ${m.join(', ') || '(any)'} → ${a.join(', ') || 'nothing yet'}`
}

export function RulesTab() {
  const { toast } = useToast()
  const { data: rules, reload } = useAsync(() => api.get<Rule[]>(`${V1}/rules`), [])
  const [edit, setEdit] = useState<Rule | 'new' | null>(null)
  const [del, setDel] = useState<Rule | null>(null)
  const toggle = async (r: Rule) => { try { await api.put(`${V1}/rules/${r.id}`, { ...r, enabled: !r.enabled }); reload() } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <div className="stack loose">
      <Section title="Rules" icon="sliders" actions={<button type="button" className="btn primary sm" onClick={() => setEdit('new')}><Icon name="plus" size={14} />Add a rule</button>}
        sub="Run on new mail as it arrives, in this order. Handy for receipts (save the PDF to Pocket Documents), newsletters (skip the inbox) or a boss (star and notify).">
        {!rules ? <Loading /> : rules.length === 0 ? <p className="muted">No rules yet.</p> : rules.map(r => (
          <div key={r.id} className={`rule ${r.enabled ? '' : 'off'}`}>
            <Switch size="sm" checked={r.enabled} onChange={() => toggle(r)} label="Enabled" />
            <div className="grow"><b>{r.name || 'Rule'}</b><div className="muted small">{describe(r)}{r.hits ? ` · used ${r.hits}×` : ''}</div></div>
            <button type="button" className="btn ghost sm" onClick={() => setEdit(r)}>Edit</button>
            <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(r)}>Remove</button>
          </div>
        ))}
      </Section>
      {edit && <RuleModal rule={edit === 'new' ? undefined : edit} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reload() }} />}
      {del && <Confirm title={`Remove “${del.name || 'this rule'}”?`} message="Mail that already arrived stays as it is." confirmLabel="Remove" onCancel={() => setDel(null)} onConfirm={async () => { const d = del; setDel(null); try { await api.del(`${V1}/rules/${d.id}`); reload() } catch (e) { toast((e as Error).message, 'err') } }} />}
    </div>
  )
}

function RuleModal({ rule, onClose, onSaved }: { rule?: Rule; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [r, setR] = useState<Rule>(rule || { id: 0, account_id: null, name: '', enabled: true, match: {}, actions: {}, hits: 0, last_hit_at: '', sort: 0, created_at: '' })
  const [apps, setApps] = useState<AppsStatus | null>(null)
  const { data: side } = useAsync(() => api.get<Sidebar>(`${V1}/sidebar`), [])
  const [busy, setBusy] = useState(false)
  useEffect(() => { loadApps().then(setApps) }, [])
  const accts = side?.accounts || []
  const folders: Folder[] = (r.account_id ? accts.filter(a => a.id === r.account_id) : accts.length === 1 ? accts : []).flatMap(a => a.folders.filter(f => f.selectable && f.role !== 'inbox'))
  const ok = (s: 'documents' | 'files' | 'photos' | 'tasks') => !!apps?.apps[s]?.configured
  const setM = (p: Partial<Rule['match']>) => setR(x => ({ ...x, match: { ...x.match, ...p } }))
  const setA = (p: Partial<Rule['actions']>) => setR(x => ({ ...x, actions: { ...x.actions, ...p } }))
  const toggleSave = (s: 'documents' | 'files' | 'photos', on: boolean) => setA({ save_to: on ? [...(r.actions.save_to || []).filter(x => x !== s), s] : (r.actions.save_to || []).filter(x => x !== s) })
  const save = async () => {
    setBusy(true)
    try {
      if (r.id) await api.put(`${V1}/rules/${r.id}`, r); else await api.post(`${V1}/rules`, r)
      toast('Rule saved'); onSaved()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const empty = !r.match.from && !r.match.to && !r.match.subject && !r.match.has_attachment && !r.match.category
  return (
    <Modal title={r.id ? 'Edit rule' : 'New rule'} onClose={onClose} size="lg" footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" disabled={busy || empty} onClick={save}>Save rule</button></>}>
      <div className="stack loose">
        <Field label="Name"><input value={r.name} onChange={e => setR({ ...r, name: e.target.value })} placeholder="Receipts to Documents" autoFocus /></Field>
        <div className="stack">
          <h4>When new mail…</h4>
          {accts.length > 1 && <Field label="Arrives in"><Select ariaLabel="Account" value={String(r.account_id || '')} onChange={v => setR({ ...r, account_id: v ? Number(v) : null, actions: { ...r.actions, move_to: '' } })} options={[{ value: '', label: 'Any account' }, ...accts.map(a => ({ value: String(a.id), label: a.label || a.email }))]} /></Field>}
          <div className="two-col tight">
            <Field label="Is from (name or address contains)"><input value={r.match.from || ''} onChange={e => setM({ from: e.target.value })} placeholder="@shop.com" /></Field>
            <Field label="Is sent to"><input value={r.match.to || ''} onChange={e => setM({ to: e.target.value })} placeholder="billing@…" /></Field>
          </div>
          <Field label="Subject contains"><input value={r.match.subject || ''} onChange={e => setM({ subject: e.target.value })} placeholder="invoice" /></Field>
          <div className="row wrap">
            <label className="check-row"><Switch size="sm" checked={!!r.match.has_attachment} onChange={v => setM({ has_attachment: v })} label="Has attachments" />Has attachments</label>
            <Select ariaLabel="Kind of mail" className="cat-select" value={r.match.category || ''} onChange={v => setM({ category: v as Category | '' })} options={[{ value: '', label: 'Any kind of mail' }, ...CATEGORIES.map(c => ({ value: c.id, label: c.label }))]} />
          </div>
          {empty && <p className="small muted">Give it at least one condition.</p>}
        </div>
        <div className="stack">
          <h4>…then</h4>
          <div className="rule-acts">
            <label className="check-row"><Switch size="sm" checked={!!r.actions.mark_read} onChange={v => setA({ mark_read: v })} label="Mark read" />Mark as read</label>
            <label className="check-row"><Switch size="sm" checked={!!r.actions.star} onChange={v => setA({ star: v })} label="Star" />Star it</label>
            <label className="check-row"><Switch size="sm" checked={!!r.actions.archive} onChange={v => setA({ archive: v, move_to: v ? '' : r.actions.move_to })} label="Archive" />Skip the inbox (archive)</label>
            <label className="check-row"><Switch size="sm" checked={!!r.actions.notify} onChange={v => setA({ notify: v })} label="Notify" />Send me a notification</label>
          </div>
          <div className="two-col tight">
            <Field label="Move to a folder">{folders.length ? <Select ariaLabel="Folder" value={r.actions.move_to || ''} onChange={v => setA({ move_to: v, archive: v ? false : r.actions.archive })} options={[{ value: '', label: 'Don’t move' }, ...folders.map(f => ({ value: f.name, label: f.display || f.name }))]} /> : <p className="muted small">{accts.length > 1 ? 'Pick one account above to choose its folder.' : 'No folders.'}</p>}</Field>
            <Field label="Put in the inbox tab"><Select ariaLabel="Category" value={r.actions.category || ''} onChange={v => setA({ category: v as Category | '' })} options={[{ value: '', label: 'Leave as it is' }, ...CATEGORIES.map(c => ({ value: c.id, label: c.label }))]} /></Field>
          </div>
          <h4 className="mt">With your Pocket apps</h4>
          <div className="rule-acts">
            {(['documents', 'files', 'photos'] as const).map(s => (
              <label key={s} className={`check-row ${ok(s) ? '' : 'dim'}`} title={ok(s) ? '' : apps?.apps[s]?.detail || 'Not connected'}>
                <Switch size="sm" disabled={!ok(s)} checked={!!r.actions.save_to?.includes(s)} onChange={v => toggleSave(s, v)} label={s} />
                Save attachments to {s === 'documents' ? 'Pocket Documents' : s === 'files' ? 'Pocket Files' : 'Photos'}
              </label>
            ))}
            <label className={`check-row ${ok('tasks') ? '' : 'dim'}`}><Switch size="sm" disabled={!ok('tasks')} checked={!!r.actions.task} onChange={v => setA({ task: v })} label="Task" />Make a task in Pocket Tasks</label>
          </div>
          {apps && !apps.core && <CoreHint />}
        </div>
      </div>
    </Modal>
  )
}

export function TemplatesManager() {
  const { toast } = useToast()
  const { data, reload } = useAsync(() => api.get<Template[]>(`${V1}/templates`), [])
  const [edit, setEdit] = useState<Partial<Template> | null>(null)
  const save = async () => {
    if (!edit) return
    try { if (edit.id) await api.put(`${V1}/templates/${edit.id}`, edit); else await api.post(`${V1}/templates`, edit); setEdit(null); reload(); toast('Template saved') } catch (e) { toast((e as Error).message, 'err') }
  }
  const del = async (t: Template) => { try { await api.del(`${V1}/templates/${t.id}`); reload() } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Section title="Templates" icon="file-text" actions={<button type="button" className="btn sm" onClick={() => setEdit({ name: '', subject: '', body_html: '' })}><Icon name="plus" size={14} />New template</button>}
      sub="Answers you write often. Insert one from the composer’s template button.">
      {!data ? <Loading /> : data.length === 0 ? <p className="muted small">No templates yet.</p> : data.map(t => (
        <div key={t.id} className="rule"><Icon name="file-text" size={15} className="muted" /><div className="grow"><b>{t.name}</b>{t.subject && <div className="muted small">{t.subject}</div>}</div>
          <button type="button" className="btn ghost sm" onClick={() => setEdit(t)}>Edit</button><button type="button" className="btn ghost sm danger-text" onClick={() => del(t)}>Remove</button></div>
      ))}
      {edit && <Modal title={edit.id ? 'Edit template' : 'New template'} onClose={() => setEdit(null)} size="lg" footer={<><button type="button" className="btn" onClick={() => setEdit(null)}>Cancel</button><button type="button" className="btn primary" disabled={!edit.name?.trim()} onClick={save}>Save</button></>}>
        <div className="stack">
          <Field label="Name"><input value={edit.name || ''} onChange={e => setEdit({ ...edit, name: e.target.value })} autoFocus /></Field>
          <Field label="Subject (optional)"><input value={edit.subject || ''} onChange={e => setEdit({ ...edit, subject: e.target.value })} /></Field>
          <Field label="Text"><RichEditor value={edit.body_html || ''} onChange={h => setEdit(x => x ? { ...x, body_html: h } : x)} minHeight={140} ariaLabel="Template text" /></Field>
        </div>
      </Modal>}
    </Section>
  )
}

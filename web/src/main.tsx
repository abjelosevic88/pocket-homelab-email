import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import './styles/base.css'
import './styles/still.css'
import './styles/player.css'
import './styles/app.css'
import './styles/settings.css'
import { AppProvider, useApp } from './lib/app-context'
import { Loading, ToastProvider } from './components/ui'
import Layout from './components/Layout'
import { ErrorBoundary } from './components/ErrorBoundary'
import Login from './pages/Login'
import Today from './pages/Today'
import Breathe, { PatternPage } from './pages/Breathe'
import Meditate from './pages/Meditate'
import Path, { ProgramPage, LessonPage } from './pages/Path'
import Journal, { PracticePage } from './pages/Journal'
import Stats from './pages/Stats'
import Milestones from './pages/Milestones'
import Session from './pages/Session'
import Settings from './pages/Settings'

function Gate({ children }: { children: React.ReactNode }) {
  const { status, user } = useApp()
  const loc = useLocation()
  if (!status) return <div className="boot"><img src="/favicon.svg" alt="" /><Loading /></div>
  if (status.needs_setup) return <Navigate to="/setup" replace />
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname + loc.search }} />
  return <>{children}</>
}

function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login mode="login" />} />
      <Route path="/setup" element={<Login mode="setup" />} />
      <Route path="/settings/:tab?" element={<Gate><ErrorBoundary where="settings"><Settings /></ErrorBoundary></Gate>} />
      <Route path="/session" element={<Gate><ErrorBoundary where="session"><Session /></ErrorBoundary></Gate>} />
      <Route element={<Gate><ErrorBoundary where="still"><Layout /></ErrorBoundary></Gate>}>
        <Route path="/" element={<Today />} />
        <Route path="/breathe" element={<Breathe />} />
        <Route path="/breathe/:key" element={<PatternPage />} />
        <Route path="/meditate" element={<Meditate />} />
        <Route path="/path" element={<Path />} />
        <Route path="/path/:program" element={<ProgramPage />} />
        <Route path="/path/:program/:lesson" element={<LessonPage />} />
        <Route path="/journal" element={<Journal />} />
        <Route path="/journal/:id" element={<PracticePage />} />
        <Route path="/stats" element={<Stats />} />
        <Route path="/milestones" element={<Milestones />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <ErrorBoundary where="app"><ToastProvider><AppProvider><App /></AppProvider></ToastProvider></ErrorBoundary>
    </BrowserRouter>
  </React.StrictMode>,
)

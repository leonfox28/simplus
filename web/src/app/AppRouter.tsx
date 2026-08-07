import { Navigate, Route, Routes } from 'react-router'
import { Spin } from 'antd'
import { lazy, Suspense } from 'react'
import { AppShell } from './AppShell'

const Calls = lazy(() => import('@/pages/Calls'))
const Dashboard = lazy(() => import('@/pages/Dashboard'))
const Lines = lazy(() => import('@/pages/Lines'))
const Login = lazy(() => import('@/pages/Login'))
const Messages = lazy(() => import('@/pages/Messages'))
const Mihomo = lazy(() => import('@/pages/Mihomo'))
const Modems = lazy(() => import('@/pages/Modems'))
const Notifications = lazy(() => import('@/pages/Notifications'))
const Settings = lazy(() => import('@/pages/Settings'))
const Setup = lazy(() => import('@/pages/Setup'))

export function AppRouter() {
  return <Suspense fallback={<div className="full-page-state"><Spin size="large" /></div>}><Routes>
    <Route path="/login" element={<Login />} />
    <Route path="/setup" element={<Setup />} />
    <Route element={<AppShell />}>
      <Route index element={<Navigate to="/dashboard" replace />} />
      <Route path="/dashboard" element={<Dashboard />} />
      <Route path="/modems" element={<Modems />} />
      <Route path="/lines" element={<Lines />} />
      <Route path="/messages" element={<Messages />} />
      <Route path="/calls" element={<Calls />} />
      <Route path="/mihomo" element={<Mihomo />} />
      <Route path="/notifications" element={<Notifications />} />
      <Route path="/settings" element={<Settings />} />
    </Route>
    <Route path="*" element={<Navigate to="/dashboard" replace />} />
  </Routes></Suspense>
}

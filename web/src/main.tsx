import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { AppProviders } from './app/AppProviders'
import './global.css'

const root = document.getElementById('root')
if (!root) throw new Error('Application root is missing')

createRoot(root).render(<StrictMode><AppProviders /></StrictMode>)

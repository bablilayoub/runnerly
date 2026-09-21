import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'

import { App } from '@/App'
import { Toaster } from '@/components/ui/sonner'
import { TooltipProvider } from '@/components/ui/tooltip'
import '@/index.css'

const root = document.getElementById('root')
if (!root) {
  throw new Error('no #root element to mount into')
}

createRoot(root).render(
  <StrictMode>
    <BrowserRouter>
      <TooltipProvider delayDuration={200}>
        <App />
        <Toaster position="bottom-right" />
      </TooltipProvider>
    </BrowserRouter>
  </StrictMode>,
)

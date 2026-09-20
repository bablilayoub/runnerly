import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom"

import App from "./App.tsx"
import { DocsPage } from "./docs/DocsPage.tsx"
import { FIRST_DOC } from "./docs/registry.ts"
import "./index.css"

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<App />} />
        <Route path="/docs" element={<Navigate to={`/docs/${FIRST_DOC}`} replace />} />
        <Route path="/docs/:slug" element={<DocsPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  </StrictMode>,
)

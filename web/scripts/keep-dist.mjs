// Recreate the marker that keeps internal/web/dist present in git.
//
// The Go server embeds that directory, and `//go:embed` fails outright when
// it does not exist. A checkout that has never built the dashboard must
// still compile, so an empty dist is committed via this marker — and Vite's
// emptyOutDir deletes everything in the directory, including it.
//
// Without this, `go vet ./...` fails on a fresh clone. That is exactly how
// it failed in CI the first time.
import { mkdirSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const marker = resolve(dirname(fileURLToPath(import.meta.url)), '../../internal/web/dist/.gitkeep')

mkdirSync(dirname(marker), { recursive: true })
writeFileSync(marker, '')

// The landing page tells people to run
//
//   curl -fsSL https://runnerly.dev/install.sh | sh
//
// so the installer has to be part of what the site publishes. Copying it at
// build time rather than keeping a second copy in public/ means the page can
// never advertise a stale installer: there is one install.sh in the
// repository, and this is where it goes.
import { copyFileSync, mkdirSync } from "node:fs"
import { dirname, join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const here = dirname(fileURLToPath(import.meta.url))
const repo = resolve(here, "..", "..")

const source = join(repo, "install.sh")
const target = join(here, "..", "dist", "install.sh")

mkdirSync(dirname(target), { recursive: true })
copyFileSync(source, target)
console.log("copied install.sh into dist/")

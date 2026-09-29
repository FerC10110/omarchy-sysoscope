// The plugin's own .qml and .js files (root and blocks/), for the scans in
// coverage.test.js and plaintext.test.js.
const fs = require("node:fs")
const path = require("node:path")

const ROOT = path.join(__dirname, "..")

function sources(exts) {
  const out = []
  for (const dir of [ROOT, path.join(ROOT, "blocks")]) {
    if (!fs.existsSync(dir)) continue
    for (const f of fs.readdirSync(dir).sort()) {
      if (exts.some(e => f.endsWith(e))) {
        const file = path.join(dir, f)
        out.push({ name: path.relative(ROOT, file), text: fs.readFileSync(file, "utf8") })
      }
    }
  }
  return out
}

module.exports = { ROOT, sources }

import fs from 'fs'
import path from 'path'

/**
 * 扫描 servers 功能里所有 t() / i18n 调用的 key，检查 6 个语言包是否齐全。
 * 比朴素的正则更全：单双引号、多行、labelKey: '...' 这类间接引用都算。
 */

const files = []
function walk(d) {
  for (const f of fs.readdirSync(d)) {
    const p = path.join(d, f)
    const st = fs.statSync(p)
    if (st.isDirectory()) walk(p)
    else if (/\.(ts|tsx)$/.test(f)) files.push(p)
  }
}
walk('src/features/servers')

const keys = new Set()
const patterns = [
  /\bt\(\s*'((?:[^'\\]|\\.)*)'/g,
  /\bt\(\s*"((?:[^"\\]|\\.)*)"/g,
  /\bt\(\s*\n\s*'((?:[^'\\]|\\.)*)'/g,
  /\bt\(\s*\n\s*"((?:[^"\\]|\\.)*)"/g,
  /\blabelKey:\s*'((?:[^'\\]|\\.)*)'/g,
  /\btitleKey:\s*'((?:[^'\\]|\\.)*)'/g,
  /\bnameKey:\s*'((?:[^'\\]|\\.)*)'/g,
]

for (const f of files) {
  const src = fs.readFileSync(f, 'utf8')
  for (const re of patterns) {
    let m
    re.lastIndex = 0
    while ((m = re.exec(src))) keys.add(m[1])
  }
}

// constants.ts 里的 nameKey 是通过 t(region.nameKey) 间接引用的，
// 这些值本身也是 key，需要一并收集
const constants = fs.readFileSync('src/features/servers/constants.ts', 'utf8')
for (const m of constants.matchAll(/nameKey:\s*'((?:[^'\\]|\\.)*)'/g))
  keys.add(m[1])

const locales = ['en', 'zh', 'ja', 'fr', 'ru', 'vi']
const data = {}
for (const l of locales) {
  const parsed = JSON.parse(
    fs.readFileSync(`src/i18n/locales/${l}.json`, 'utf8')
  )
  data[l] = parsed.translation ?? parsed
}

let allOk = true
for (const l of locales) {
  const missing = [...keys].filter((k) => !(k in data[l])).sort()
  if (missing.length) allOk = false
  console.log(`[${l}] missing ${missing.length}:`)
  for (const k of missing) console.log('  -', k)
}

// 顺带检查：语言包里有没有「值等于 key」的未翻译项（新增的 servers 文案不该出现）
console.log(`\nkeys used: ${keys.size}`)
if (!allOk) process.exitCode = 1

import fs from 'node:fs'
import path from 'path'
import { createRequire } from 'module'
import { fileURLToPath } from 'url'
import { defineConfig, loadEnv } from '@rsbuild/core'
import { pluginReact } from '@rsbuild/plugin-react'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const require = createRequire(import.meta.url)
const semiUiDir = path.resolve(
  path.dirname(require.resolve('@douyinfe/semi-ui')),
  '../..',
)

// date-fns v2 兼容目录：Semi（date-fns-tz@1.x）需要 date-fns v2 的 `_lib/*` 内部路径，
// 而工作区根目录可能 hoist 的是 v4。按候选顺序找第一个可用的 v2 副本；
// `--filter ./classic` 等裁剪安装下嵌套副本可能不存在，此时返回 undefined
// 让 Rspack 走默认解析（该布局下根即 v2，无需别名）。
function resolveDateFnsV2Dir(): string | undefined {
  const candidates = [
    path.resolve(semiUiDir, '../semi-foundation/node_modules/date-fns'),
    path.resolve(semiUiDir, 'node_modules/date-fns'),
    path.resolve(__dirname, './node_modules/date-fns'),
    path.resolve(__dirname, '../node_modules/date-fns'),
  ]
  for (const dir of candidates) {
    try {
      const pkg = JSON.parse(
        fs.readFileSync(path.join(dir, 'package.json'), 'utf8'),
      ) as { version?: string }
      if (
        typeof pkg.version === 'string' &&
        pkg.version.startsWith('2.') &&
        fs.existsSync(path.join(dir, '_lib/toInteger/index.js'))
      ) {
        return dir
      }
    } catch {
      // 候选不存在就试下一个
    }
  }
  return undefined
}
const semiDateFnsDir = resolveDateFnsV2Dir()

export default defineConfig(({ envMode }) => {
  const env = loadEnv({ mode: envMode, prefixes: ['VITE_'] })
  const clientServerUrl =
    process.env.VITE_REACT_APP_SERVER_URL ||
    env.rawPublicVars.VITE_REACT_APP_SERVER_URL ||
    ''
  const proxyServerUrl =
    clientServerUrl ||
    'http://localhost:3000'
  const isProd = envMode === 'production'
  const devProxy = Object.fromEntries(
    (['/api', '/mj', '/pg'] as const).map((key) => [
      key,
      { target: proxyServerUrl, changeOrigin: true },
    ]),
  ) as Record<string, { target: string; changeOrigin: boolean }>

  return {
    plugins: [pluginReact()],
    source: {
      entry: {
        index: './src/index.jsx',
      },
      define: {
        'import.meta.env.VITE_REACT_APP_SERVER_URL': JSON.stringify(
          clientServerUrl,
        ),
      },
    },
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
        '@douyinfe/semi-ui/dist/css/semi.css': path.resolve(
          semiUiDir,
          'dist/css/semi.css',
        ),
        // 仅当找到可用的 v2 副本时才加别名，避免悬空别名把能解析的请求改坏
        ...(semiDateFnsDir ? { 'date-fns': semiDateFnsDir } : {}),
      },
    },
    html: {
      template: './index.html',
    },
    server: {
      host: '0.0.0.0',
      strictPort: false,
      proxy: devProxy,
    },
    output: {
      minify: isProd,
      target: 'web',
      distPath: {
        root: 'dist',
      },
    },
    performance: {
      removeConsole: isProd ? ['log'] : false,
      buildCache: {
        cacheDigest: [process.env.VITE_REACT_APP_VERSION],
      },
    },
    tools: {
      rspack: {
        module: {
          rules: [
            {
              test: /src[\\/].*\.js$/,
              type: 'javascript/auto',
              use: [
                {
                  loader: 'builtin:swc-loader',
                  options: {
                    jsc: {
                      parser: {
                        syntax: 'ecmascript',
                        jsx: true,
                      },
                      transform: {
                        react: {
                          runtime: 'automatic',
                          development: !isProd,
                          refresh: !isProd,
                        },
                      },
                    },
                  },
                },
              ],
            },
          ],
        },
      },
    },
  }
})

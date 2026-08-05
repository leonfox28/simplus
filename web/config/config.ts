import { defineConfig } from '@umijs/max'
import routes from './routes'

const target = process.env.VITE_API_PROXY_TARGET ?? 'http://127.0.0.1:8080'

export default defineConfig({
  hash: true,
  esbuildMinifyIIFE: true,
  routes,
  title: 'Simplus',
  locale: { default: 'zh-CN', antd: true, baseNavigator: true },
  initialState: {},
  model: {},
  reactQuery: {},
  antd: { configProvider: { theme: { token: { colorPrimary: '#1677ff', borderRadius: 8 } } } },
  layout: { title: 'Simplus', locale: true, navTheme: 'light', layout: 'side', contentWidth: 'Fluid', fixedHeader: true, fixSiderbar: true },
  proxy: { '/api': { target, changeOrigin: true } },
})

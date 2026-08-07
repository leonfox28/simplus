import { App as AntdApp, ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { QueryClientProvider } from '@tanstack/react-query'
import { useState } from 'react'
import { BrowserRouter } from 'react-router'
import { configureApiClient } from '@/api/setupClient'
import { createAppQueryClient } from '@/api/queryClient'
import { AppRouter } from './AppRouter'
import { BootstrapGate } from './BootstrapGate'

configureApiClient()

export function AppProviders() {
  const [queryClient] = useState(createAppQueryClient)
  return <ConfigProvider locale={zhCN} theme={{ token: { colorPrimary: '#1677ff', borderRadius: 8 } }}>
    <AntdApp>
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          <BootstrapGate><AppRouter /></BootstrapGate>
        </BrowserRouter>
      </QueryClientProvider>
    </AntdApp>
  </ConfigProvider>
}

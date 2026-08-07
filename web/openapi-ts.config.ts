import { defineConfig } from '@hey-api/openapi-ts'

export default defineConfig({
  input: '../api/openapi.yaml',
  output: 'src/api/generated',
  plugins: [
    '@hey-api/typescript',
    {
      name: '@hey-api/client-fetch',
      runtimeConfigPath: './src/api/runtime.ts',
    },
    {
      name: 'zod',
      requests: true,
      responses: true,
      definitions: true,
    },
    {
      name: '@hey-api/sdk',
      validator: true,
    },
    {
      name: '@tanstack/react-query',
      queryKeys: { tags: true },
      queryOptions: true,
      infiniteQueryKeys: { tags: true },
      infiniteQueryOptions: true,
      mutationOptions: true,
    },
  ],
})

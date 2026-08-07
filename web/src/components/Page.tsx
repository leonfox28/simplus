import { Alert, Button, Card, Empty, Flex, Grid, Skeleton, Space, Table, Typography } from 'antd'
import type { ReactNode } from 'react'
import { displayApiError } from '@/api/errors'
import type { TableColumnsType } from 'antd'

export function PageHeader({
  title,
  subtitle,
  extra,
}: {
  title: string
  subtitle?: string
  extra?: ReactNode
}) {
  return (
    <Flex className="page-header" align="flex-start" justify="space-between" gap="middle" wrap>
      <div>
        <Typography.Title level={2}>{title}</Typography.Title>
        {subtitle && <Typography.Paragraph type="secondary">{subtitle}</Typography.Paragraph>}
      </div>
      {extra && <Space wrap>{extra}</Space>}
    </Flex>
  )
}

export function PageSection({
  title,
  extra,
  children,
  className,
}: {
  title?: ReactNode
  extra?: ReactNode
  children: ReactNode
  className?: string
}) {
  return <Card title={title} extra={extra} className={className}>{children}</Card>
}

export function AsyncState({
  loading,
  error,
  empty,
  emptyText = '暂无数据',
  onRetry,
  children,
}: {
  loading: boolean
  error?: unknown
  empty?: boolean
  emptyText?: string
  onRetry?: () => void
  children: ReactNode
}) {
  if (loading) return <Skeleton active paragraph={{ rows: 4 }} />
  if (error) {
    return <Alert
      type="error"
      showIcon
      title={displayApiError(error)}
      action={onRetry && <Button size="small" onClick={onRetry}>重试</Button>}
    />
  }
  if (empty) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={emptyText} />
  return <>{children}</>
}

export function ResponsiveDataView<T extends object>({
  data,
  columns,
  rowKey,
  renderCard,
  loading = false,
  emptyText = '暂无数据',
}: {
  data: readonly T[]
  columns: TableColumnsType<T>
  rowKey: keyof T | ((record: T) => string)
  renderCard: (record: T) => ReactNode
  loading?: boolean
  emptyText?: string
}) {
  const compact = !Grid.useBreakpoint().md
  if (compact) {
    if (loading) return <Skeleton active paragraph={{ rows: 4 }} />
    if (!data.length) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={emptyText} />
    return <div className="responsive-card-list" role="list">{data.map((item) => {
      const key = typeof rowKey === 'function' ? rowKey(item) : String(item[rowKey])
      return <div className="responsive-list-item" role="listitem" key={key}>{renderCard(item)}</div>
    })}</div>
  }
  return <div className="table-scroll"><Table<T>
    rowKey={rowKey as never}
    columns={columns}
    dataSource={[...data]}
    loading={loading}
    pagination={false}
    scroll={{ x: 'max-content' }}
    locale={{ emptyText }}
  /></div>
}

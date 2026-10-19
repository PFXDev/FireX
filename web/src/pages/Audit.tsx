import { useCallback, useEffect, useState } from 'react'
import { HistoryIcon } from 'lucide-react'

import { type AuditLog, api } from '@/api'
import { LoadErrorAlert, RefreshButton, TableSkeleton } from '@/components/load-state'
import { PageHeader } from '@/components/page-header'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Spinner } from '@/components/ui/spinner'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { errorMessage, formatTime } from '@/lib/format'

type Page = { items: AuditLog[]; more: boolean }

export function AuditPage() {
  const [items, setItems] = useState<AuditLog[]>([])
  const [more, setMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    setLoadError(null)
    try {
      const page = await api.get<Page>('/audit-logs')
      setItems(page.items)
      setMore(page.more)
    } catch (err) {
      setLoadError(errorMessage(err, '加载操作日志失败'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const loadMore = async () => {
    const last = items[items.length - 1]
    if (!last) return
    setLoadingMore(true)
    try {
      const page = await api.get<Page>(`/audit-logs?before=${last.id}`)
      setItems((current) => [...current, ...page.items])
      setMore(page.more)
    } catch (err) {
      setLoadError(errorMessage(err, '加载操作日志失败'))
    } finally {
      setLoadingMore(false)
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="操作日志" description="管理后台里每一次成功的配置修改：谁、什么时间、对什么做了什么。" />
      <Card>
        <CardHeader>
          <CardTitle>最近的修改</CardTitle>
          <CardDescription>只记录操作和对象，不记录提交的内容；保留最近一万条。</CardDescription>
          <CardAction>
            <RefreshButton loading={loading} onClick={() => void refresh()} />
          </CardAction>
        </CardHeader>
        <CardContent className="px-0">
          {loading && items.length === 0 ? (
            <TableSkeleton
              columns={[
                { label: '时间', width: 'w-32' },
                { label: '操作', width: 'w-24' },
                { label: '对象', width: 'w-24' },
                { label: '管理员', className: 'hidden sm:table-cell', width: 'w-16' },
                { label: 'IP', className: 'hidden md:table-cell', width: 'w-24' },
              ]}
            />
          ) : loadError && items.length === 0 ? (
            <div className="px-4">
              <LoadErrorAlert title="操作日志加载失败" message={loadError} retrying={loading} onRetry={() => void refresh()} />
            </div>
          ) : items.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <HistoryIcon />
                </EmptyMedia>
                <EmptyTitle>还没有记录</EmptyTitle>
                <EmptyDescription>在后台修改配置后，记录会出现在这里。</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <div className="flex flex-col gap-3">
              {loadError && (
                <div className="px-4">
                  <LoadErrorAlert title="加载失败" message={loadError} />
                </div>
              )}
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>时间</TableHead>
                    <TableHead>操作</TableHead>
                    <TableHead>对象</TableHead>
                    <TableHead className="hidden sm:table-cell">管理员</TableHead>
                    <TableHead className="hidden md:table-cell">IP</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((item) => (
                    <TableRow key={item.id}>
                      <TableCell className="text-muted-foreground tabular-nums">{formatTime(item.createdAt)}</TableCell>
                      <TableCell className="font-medium">{item.action}</TableCell>
                      <TableCell className="max-w-64 truncate" title={item.target}>
                        {item.target || <span className="text-muted-foreground">—</span>}
                      </TableCell>
                      <TableCell className="hidden sm:table-cell">{item.actor}</TableCell>
                      <TableCell className="hidden font-mono text-xs text-muted-foreground md:table-cell">{item.ip}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              {more && (
                <div className="flex justify-center px-4">
                  <Button variant="outline" size="sm" disabled={loadingMore} onClick={() => void loadMore()}>
                    {loadingMore && <Spinner data-icon="inline-start" />}
                    {loadingMore ? '加载中…' : '加载更多'}
                  </Button>
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

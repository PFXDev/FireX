import { useCallback, useEffect, useMemo, useState } from 'react'
import { HistoryIcon, SearchXIcon, XIcon } from 'lucide-react'

import { type AuditLog, type AuditLogPage, api } from '@/api'
import { LoadErrorAlert, RefreshButton, TableSkeleton } from '@/components/load-state'
import { PageHeader } from '@/components/page-header'
import { SearchInput, TableToolbar } from '@/components/table-toolbar'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { errorMessage, formatTime } from '@/lib/format'

function auditUrl(q: string, actor: string, action: string, before?: number) {
  const params = new URLSearchParams()
  if (q.trim()) params.set('q', q.trim())
  if (actor !== 'all') params.set('actor', actor)
  if (action !== 'all') params.set('action', action)
  if (before !== undefined) params.set('before', String(before))
  const qs = params.toString()
  return qs ? `/audit-logs?${qs}` : '/audit-logs'
}

export function AuditPage() {
  const [items, setItems] = useState<AuditLog[]>([])
  const [more, setMore] = useState(false)
  const [total, setTotal] = useState<number | undefined>(undefined)
  const [actors, setActors] = useState<string[]>([])
  const [actions, setActions] = useState<string[]>([])

  const [query, setQuery] = useState('')
  const [debouncedQuery, setDebouncedQuery] = useState('')
  const [actorFilter, setActorFilter] = useState<string>('all')
  const [actionFilter, setActionFilter] = useState<string>('all')

  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)

  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedQuery(query), 300)
    return () => window.clearTimeout(timer)
  }, [query])

  const refresh = useCallback(async () => {
    setLoading(true)
    setLoadError(null)
    try {
      const page = await api.get<AuditLogPage>(auditUrl(debouncedQuery, actorFilter, actionFilter))
      setItems(page.items)
      setMore(page.more)
      setTotal(page.total)
      if (page.actors) setActors(page.actors)
      if (page.actions) setActions(page.actions)
    } catch (err) {
      setLoadError(errorMessage(err, '加载操作日志失败'))
    } finally {
      setLoading(false)
    }
  }, [debouncedQuery, actorFilter, actionFilter])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const loadMore = async () => {
    const last = items[items.length - 1]
    if (!last) return
    setLoadingMore(true)
    try {
      const page = await api.get<AuditLogPage>(auditUrl(debouncedQuery, actorFilter, actionFilter, last.id))
      setItems((current) => [...current, ...page.items])
      setMore(page.more)
      setTotal(page.total)
    } catch (err) {
      setLoadError(errorMessage(err, '加载操作日志失败'))
    } finally {
      setLoadingMore(false)
    }
  }

  const actorItems = useMemo(
    () => [
      { label: '全部管理员', value: 'all' },
      ...actors.map((actor) => ({ label: actor, value: actor })),
    ],
    [actors],
  )

  const actionItems = useMemo(
    () => [
      { label: '全部操作', value: 'all' },
      ...actions.map((action) => ({ label: action, value: action })),
    ],
    [actions],
  )

  const hasActiveFilters = Boolean(query.trim() || actorFilter !== 'all' || actionFilter !== 'all')

  const resetFilters = () => {
    setQuery('')
    setDebouncedQuery('')
    setActorFilter('all')
    setActionFilter('all')
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
        <CardContent className="flex flex-col gap-4 px-0">
          <TableToolbar>
            <SearchInput
              value={query}
              onChange={setQuery}
              placeholder="按操作、对象或 IP 搜索…"
              ariaLabel="搜索操作日志"
              clearLabel="清除搜索"
            />
            <Select
              items={actorItems}
              value={actorFilter}
              onValueChange={(value) => setActorFilter(value ?? 'all')}
            >
              <SelectTrigger className="w-full sm:w-40" aria-label="按管理员筛选">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {actorItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <Select
              items={actionItems}
              value={actionFilter}
              onValueChange={(value) => setActionFilter(value ?? 'all')}
            >
              <SelectTrigger className="w-full sm:w-48" aria-label="按操作类型筛选">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {actionItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            {hasActiveFilters && (
              <Button type="button" variant="ghost" size="sm" onClick={resetFilters}>
                <XIcon data-icon="inline-start" />
                清除筛选
              </Button>
            )}
          </TableToolbar>

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
          ) : items.length === 0 && hasActiveFilters ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <SearchXIcon />
                </EmptyMedia>
                <EmptyTitle>没有匹配的日志记录</EmptyTitle>
                <EmptyDescription>更换关键词或放宽管理员、操作筛选条件再试。</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button type="button" variant="outline" onClick={resetFilters}>
                  <XIcon data-icon="inline-start" />
                  清除筛选
                </Button>
              </EmptyContent>
            </Empty>
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
                      <TableCell className="max-w-64 whitespace-normal">
                        <div className="flex flex-col gap-0.5">
                          <span className="truncate" title={item.target}>
                            {item.target || <span className="text-muted-foreground">—</span>}
                          </span>
                          <span className="text-xs text-muted-foreground sm:hidden">{item.actor}</span>
                        </div>
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
        <CardFooter className="text-muted-foreground">
          {loading && items.length === 0
            ? '正在加载操作日志'
            : `已显示 ${items.length} / 共 ${total ?? items.length} 条记录`}
        </CardFooter>
      </Card>
    </div>
  )
}

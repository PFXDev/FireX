import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'
import { DatabaseIcon, DownloadIcon, PlusIcon, RotateCcwIcon, TrashIcon, TriangleAlertIcon, UploadIcon } from 'lucide-react'

import { api } from '@/api'
import type { BackupEntry, BackupList, RestoreResult } from '@/api'
import { CodeText } from '@/components/code-display'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { LoadErrorAlert, RefreshButton, TableSkeleton } from '@/components/load-state'
import { PageHeader } from '@/components/page-header'
import { StatusBadge, type Tone } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Spinner } from '@/components/ui/spinner'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { errorMessage, formatBytes, formatTime } from '@/lib/format'

const KINDS: Record<BackupEntry['kind'], { label: string; tone: Tone }> = {
  auto: { label: '自动', tone: 'idle' },
  manual: { label: '手动', tone: 'good' },
  upload: { label: '上传', tone: 'idle' },
  'pre-restore': { label: '还原前', tone: 'warn' },
}

export function BackupPage() {
  const [data, setData] = useState<BackupList | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [restoring, setRestoring] = useState<string | null>(null)
  const [toRestore, setToRestore] = useState<string | null>(null)
  const [toDelete, setToDelete] = useState<string | null>(null)
  const uploadRef = useRef<HTMLInputElement>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setLoadError(null)
    try {
      setData(await api.get<BackupList>('/backups'))
    } catch (err) {
      setLoadError(errorMessage(err, '加载备份列表失败'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  // The restored database signs every admin out, so the first 401 means the
  // restarted server is up; reloading lands on the login form.
  useEffect(() => {
    if (!restoring) return
    const timer = setInterval(() => {
      fetch('/api/auth/me', { credentials: 'include' })
        .then((res) => {
          if (res.status === 401) window.location.reload()
        })
        .catch(() => {
          // Down mid-restart; keep polling until it answers.
        })
    }, 1500)
    return () => clearInterval(timer)
  }, [restoring])

  const create = async () => {
    setCreating(true)
    try {
      const created = await api.post<BackupEntry>('/backups')
      toast.success(`已创建备份 ${created.name}`)
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '创建备份失败'))
    } finally {
      setCreating(false)
    }
  }

  const upload = async (file: File) => {
    setUploading(true)
    try {
      const form = new FormData()
      form.append('file', file)
      const uploaded = await api.post<BackupEntry>('/backups/upload', form)
      toast.success(`已上传为 ${uploaded.name}，可从列表中还原`)
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '上传备份失败'))
    } finally {
      setUploading(false)
      if (uploadRef.current) uploadRef.current.value = ''
    }
  }

  const restore = async (name: string) => {
    try {
      const result = await api.post<RestoreResult>(`/backups/${encodeURIComponent(name)}/restore`)
      setRestoring(name)
      toast.success(`当前数据已另存为 ${result.preRestore}，FireX 正在重启`)
    } catch (err) {
      toast.error(errorMessage(err, '还原失败'))
      throw err
    }
  }

  const remove = async (name: string) => {
    try {
      await api.del(`/backups/${encodeURIComponent(name)}`)
      toast.success(`已删除备份 ${name}`)
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '删除备份失败'))
      throw err
    }
  }

  const items = data?.items ?? []
  const locked = Boolean(restoring) || Boolean(data?.pendingRestore)

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="数据库备份" description="定时自动备份数据库，也可手动备份、上传其他实例的备份，并从任一备份还原。">
        <Button variant="outline" disabled={uploading || locked} onClick={() => uploadRef.current?.click()}>
          {uploading ? <Spinner data-icon="inline-start" /> : <UploadIcon data-icon="inline-start" />}
          上传备份
        </Button>
        <Button disabled={creating || locked} onClick={() => void create()}>
          {creating ? <Spinner data-icon="inline-start" /> : <PlusIcon data-icon="inline-start" />}
          立即备份
        </Button>
        <input
          ref={uploadRef}
          type="file"
          accept=".db,application/vnd.sqlite3,application/x-sqlite3"
          className="hidden"
          onChange={(e) => {
            const file = e.target.files?.[0]
            if (file) void upload(file)
          }}
        />
      </PageHeader>

      {restoring ? (
        <Alert>
          <Spinner />
          <AlertTitle>正在还原 {restoring}</AlertTitle>
          <AlertDescription>FireX 正在重启。还原会注销所有管理员，服务恢复后页面将自动刷新到登录页。</AlertDescription>
        </Alert>
      ) : (
        data?.pendingRestore && (
          <Alert variant="warning">
            <TriangleAlertIcon />
            <AlertTitle>有一次还原等待重启</AlertTitle>
            <AlertDescription>
              FireX 下次启动时会用已暂存的备份替换数据库。若不想还原，请在重启前删除数据库文件旁的{' '}
              <CodeText>.restore</CodeText> 和 <CodeText>.restore.json</CodeText> 文件。
            </AlertDescription>
          </Alert>
        )
      )}

      <Card>
        <CardHeader>
          <CardTitle>备份列表</CardTitle>
          <CardDescription>
            只有自动备份会按“保留份数”清理，其余备份需手动删除。间隔和份数在“系统”页的服务端配置中设置。
            {data && (
              <>
                {' '}存储目录 <CodeText>{data.dir}</CodeText>
              </>
            )}
          </CardDescription>
          <CardAction>
            <RefreshButton loading={loading} onClick={() => void load()} />
          </CardAction>
        </CardHeader>
        <CardContent className="px-0">
          {loading && !data ? (
            <TableSkeleton
              columns={[
                { label: '名称', width: 'w-48' },
                { label: '类型', width: 'w-12' },
                { label: '大小', className: 'hidden sm:table-cell', width: 'w-16' },
                { label: '创建时间', className: 'hidden md:table-cell', width: 'w-32' },
              ]}
            />
          ) : loadError && !data ? (
            <div className="px-4">
              <LoadErrorAlert title="备份列表加载失败" message={loadError} retrying={loading} onRetry={() => void load()} />
            </div>
          ) : items.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <DatabaseIcon />
                </EmptyMedia>
                <EmptyTitle>还没有备份</EmptyTitle>
                <EmptyDescription>开启自动备份后会按间隔生成，也可以点击“立即备份”。</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <div className="flex flex-col gap-3">
              {loadError && (
                <div className="px-4">
                  <LoadErrorAlert title="刷新失败" message={loadError} />
                </div>
              )}
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>名称</TableHead>
                    <TableHead>类型</TableHead>
                    <TableHead className="hidden sm:table-cell">大小</TableHead>
                    <TableHead className="hidden md:table-cell">创建时间</TableHead>
                    <TableHead className="text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((entry) => (
                    <TableRow key={entry.name}>
                      <TableCell className="font-mono text-xs">{entry.name}</TableCell>
                      <TableCell>
                        <StatusBadge tone={KINDS[entry.kind].tone}>{KINDS[entry.kind].label}</StatusBadge>
                      </TableCell>
                      <TableCell className="hidden text-muted-foreground tabular-nums sm:table-cell">
                        {formatBytes(entry.size)}
                      </TableCell>
                      <TableCell className="hidden text-muted-foreground tabular-nums md:table-cell">
                        {formatTime(entry.createdAt)}
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center justify-end gap-1">
                          <Button
                            variant="ghost"
                            size="sm"
                            render={<a href={`/api/backups/${encodeURIComponent(entry.name)}/download`} download />}
                            nativeButton={false}
                          >
                            <DownloadIcon data-icon="inline-start" />
                            下载
                          </Button>
                          <Button variant="ghost" size="sm" disabled={locked} onClick={() => setToRestore(entry.name)}>
                            <RotateCcwIcon data-icon="inline-start" />
                            还原
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="text-destructive hover:text-destructive"
                            disabled={locked}
                            onClick={() => setToDelete(entry.name)}
                          >
                            <TrashIcon data-icon="inline-start" />
                            删除
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>

      <ConfirmDialog
        open={toRestore !== null}
        onOpenChange={(open) => !open && setToRestore(null)}
        title="还原数据库？"
        description={
          <>
            将用 <strong>{toRestore}</strong> 替换当前数据库并重启 FireX，之后所有管理员需要重新登录。还原前会先把当前数据另存为一份“还原前”备份，需要时可再从它还原回来。
          </>
        }
        confirmLabel="还原并重启"
        onConfirm={async () => {
          if (toRestore) await restore(toRestore)
        }}
      />

      <ConfirmDialog
        open={toDelete !== null}
        onOpenChange={(open) => !open && setToDelete(null)}
        title="删除备份？"
        description={
          <>
            备份 <strong>{toDelete}</strong> 将从磁盘上删除，无法恢复。
          </>
        }
        confirmLabel="删除"
        onConfirm={async () => {
          if (toDelete) await remove(toDelete)
        }}
      />
    </div>
  )
}

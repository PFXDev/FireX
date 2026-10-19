import { useCallback, useEffect, useState } from 'react'
import { KeyRoundIcon, PencilIcon, PlusIcon, ShieldCheckIcon, ShieldIcon, Trash2Icon, TriangleAlertIcon } from 'lucide-react'
import { toast } from 'sonner'

import { type AdminAccount, type AdminRole, api } from '@/api'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { LoadErrorAlert, RefreshButton, TableSkeleton } from '@/components/load-state'
import { PageHeader } from '@/components/page-header'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { errorMessage, formatTime } from '@/lib/format'
import { useSession } from '@/lib/session'

type Draft = {
  id?: number
  username: string
  password: string
  role: AdminRole
}

const emptyDraft: Draft = { username: '', password: '', role: 'operator' }

const ROLE_LABEL: Record<AdminRole, string> = {
  super_admin: '超级管理员',
  operator: '普通管理员',
}

export function AdminsPage() {
  const { username } = useSession()
  const [admins, setAdmins] = useState<AdminAccount[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [showErrors, setShowErrors] = useState(false)
  const [saving, setSaving] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<AdminAccount | null>(null)
  const [resetTarget, setResetTarget] = useState<AdminAccount | null>(null)
  const [newPassword, setNewPassword] = useState('')
  const [resetBusy, setResetBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      setAdmins(await api.get<AdminAccount[]>('/admins'))
      setLoadError(null)
      return true
    } catch (err) {
      setLoadError(errorMessage(err, '管理员列表加载失败'))
      return false
    }
  }, [])

  useEffect(() => {
    void load().finally(() => setLoading(false))
  }, [load])

  const refresh = async () => {
    setRefreshing(true)
    await load()
    setRefreshing(false)
  }

  const revalidate = async () => {
    if (!(await load())) toast.error('操作已完成，但列表刷新失败，请手动重试')
  }

  const openCreate = () => {
    setShowErrors(false)
    setDraft({ ...emptyDraft })
  }

  const openEdit = (admin: AdminAccount) => {
    setShowErrors(false)
    setDraft({ id: admin.id, username: admin.username, password: '', role: admin.role })
  }

  const save = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!draft || saving) return
    setShowErrors(true)
    if (!draft.username.trim()) return
    if (!draft.id && draft.password.length < 8) return
    if (!event.currentTarget.reportValidity()) return

    setSaving(true)
    try {
      if (draft.id) {
        await api.put(`/admins/${draft.id}`, { username: draft.username.trim(), role: draft.role })
        toast.success('管理员已保存')
      } else {
        await api.post('/admins', {
          username: draft.username.trim(),
          password: draft.password,
          role: draft.role,
        })
        toast.success('管理员已创建')
      }
      setDraft(null)
      await revalidate()
    } catch (err) {
      toast.error(errorMessage(err, '保存失败'))
    } finally {
      setSaving(false)
    }
  }

  const remove = async (admin: AdminAccount) => {
    try {
      await api.del(`/admins/${admin.id}`)
      toast.success('管理员已删除')
      await revalidate()
    } catch (err) {
      toast.error(errorMessage(err, '删除失败'))
      throw err
    }
  }

  const resetPassword = async () => {
    if (!resetTarget || newPassword.length < 8) return
    setResetBusy(true)
    try {
      await api.post(`/admins/${resetTarget.id}/resetPassword`, { password: newPassword })
      toast.success(`已重置 ${resetTarget.username} 的密码，其所有会话都已失效`)
      setResetTarget(null)
      setNewPassword('')
      await revalidate()
    } catch (err) {
      toast.error(errorMessage(err, '重置密码失败'))
    } finally {
      setResetBusy(false)
    }
  }

  const superAdmins = admins.filter((admin) => admin.role === 'super_admin').length
  const usernameInvalid = Boolean(showErrors && draft && !draft.username.trim())
  const passwordInvalid = Boolean(showErrors && draft && !draft.id && draft.password.length < 8)
  const resetInvalid = newPassword.length > 0 && newPassword.length < 8

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="管理员" description="超级管理员拥有全部权限；普通管理员只能管理用户，并只读查看总览与套餐。">
        <Button onClick={openCreate}>
          <PlusIcon data-icon="inline-start" />
          新建管理员
        </Button>
      </PageHeader>

      <Alert>
        <ShieldCheckIcon />
        <AlertTitle>权限边界</AlertTitle>
        <AlertDescription>
          <p>超级管理员可以管理面板、入站、节点组、分流、模板、服务设置、系统更新，以及这里的其他管理员。</p>
          <p>普通管理员只能管理用户：增删改用户、重置流量、重新下发、查看订阅。概览与套餐对他们只读，其余页面一律不出现。</p>
        </AlertDescription>
      </Alert>

      <Card>
        <CardHeader>
          <CardTitle>管理员账号</CardTitle>
          <CardDescription>
            {loading || (loadError && admins.length === 0)
              ? '账号密码只保存在本机数据库，泄露时立即重置。'
              : `共 ${admins.length} 个账号，其中 ${superAdmins} 个超级管理员。最后一个超级管理员不能被降级或删除。`}
          </CardDescription>
          <CardAction>
            <RefreshButton loading={loading || refreshing} disabled={saving} onClick={() => void refresh()} />
          </CardAction>
        </CardHeader>
        <CardContent className="px-0">
          {loading ? (
            <TableSkeleton
              columns={[
                { label: '用户名', width: 'w-28' },
                { label: '角色', width: 'w-24' },
                { label: '创建时间', className: 'hidden md:table-cell', width: 'w-32' },
                { label: '最近修改', className: 'hidden lg:table-cell', width: 'w-32' },
                { width: 'w-40' },
              ]}
            />
          ) : loadError && admins.length === 0 ? (
            <div className="px-4">
              <LoadErrorAlert title="管理员列表加载失败" message={loadError} retrying={refreshing} onRetry={() => void refresh()} />
            </div>
          ) : admins.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ShieldIcon />
                </EmptyMedia>
                <EmptyTitle>还没有管理员记录</EmptyTitle>
                <EmptyDescription>正常安装至少会有一个超级管理员；尝试刷新列表。</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button onClick={() => void refresh()} disabled={refreshing}>
                  {refreshing ? <Spinner data-icon="inline-start" /> : <ShieldIcon data-icon="inline-start" />}
                  刷新
                </Button>
              </EmptyContent>
            </Empty>
          ) : (
            <div className="flex flex-col gap-3">
              {loadError && (
                <div className="px-4">
                  <Alert variant="warning">
                    <TriangleAlertIcon />
                    <AlertTitle>列表可能不是最新状态</AlertTitle>
                    <AlertDescription>{loadError}</AlertDescription>
                  </Alert>
                </div>
              )}
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>用户名</TableHead>
                    <TableHead>角色</TableHead>
                    <TableHead className="hidden md:table-cell">创建时间</TableHead>
                    <TableHead className="hidden lg:table-cell">最近修改</TableHead>
                    <TableHead>
                      <span className="sr-only">操作</span>
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {admins.map((admin) => (
                    <TableRow key={admin.id}>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <span className="font-medium">{admin.username}</span>
                          {admin.isSelf && (
                            <span className="text-xs text-muted-foreground">（当前登录{admin.username === username ? '' : '的账号'}）</span>
                          )}
                        </div>
                      </TableCell>
                      <TableCell>
                        <StatusBadge tone={admin.role === 'super_admin' ? 'good' : 'idle'}>
                          {ROLE_LABEL[admin.role] ?? admin.role}
                        </StatusBadge>
                      </TableCell>
                      <TableCell className="hidden text-muted-foreground tabular-nums md:table-cell">
                        {admin.createdAt ? formatTime(admin.createdAt) : '—'}
                      </TableCell>
                      <TableCell className="hidden text-muted-foreground tabular-nums lg:table-cell">
                        {admin.updatedAt ? formatTime(admin.updatedAt) : '—'}
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-1">
                          <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            aria-label={`编辑 ${admin.username}`}
                            onClick={() => openEdit(admin)}
                          >
                            <PencilIcon data-icon="inline-start" />
                            <span className="hidden sm:inline">编辑</span>
                          </Button>
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            disabled={admin.isSelf}
                            aria-label={`重置 ${admin.username} 的密码`}
                            onClick={() => {
                              setNewPassword('')
                              setResetTarget(admin)
                            }}
                          >
                            <KeyRoundIcon data-icon="inline-start" />
                            <span className="hidden sm:inline">重置密码</span>
                          </Button>
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            disabled={admin.isSelf}
                            aria-label={`删除 ${admin.username}`}
                            onClick={() => setPendingDelete(admin)}
                          >
                            <Trash2Icon data-icon="inline-start" />
                            <span className="hidden sm:inline">删除</span>
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
        <CardFooter className="text-muted-foreground">
          修改自己的密码请到「设置 → 账号安全」；修改后所有会话都会失效。
        </CardFooter>
      </Card>

      <Dialog
        open={draft !== null}
        onOpenChange={(open) => {
          if (!open && !saving) {
            setDraft(null)
            setShowErrors(false)
          }
        }}
      >
        <DialogContent>
          {draft && (
            <form className="contents" noValidate onSubmit={save}>
              <DialogHeader>
                <DialogTitle>{draft.id ? `编辑管理员 ${draft.username}` : '新建管理员'}</DialogTitle>
                <DialogDescription>
                  普通管理员只能管理用户和只读查看概览、套餐；超级管理员拥有全部权限。
                </DialogDescription>
              </DialogHeader>
              <FieldGroup>
                <Field data-invalid={usernameInvalid || undefined} data-disabled={saving || undefined}>
                  <FieldLabel htmlFor="admin-username">用户名</FieldLabel>
                  <Input
                    id="admin-username"
                    value={draft.username}
                    required
                    autoComplete="off"
                    aria-invalid={usernameInvalid || undefined}
                    disabled={saving}
                    onChange={(event) => setDraft({ ...draft, username: event.target.value })}
                  />
                  <FieldDescription>2–40 位字母、数字、连字符、下划线或点。</FieldDescription>
                  {usernameInvalid && <FieldError>请输入用户名。</FieldError>}
                </Field>

                {!draft.id && (
                  <Field data-invalid={passwordInvalid || undefined} data-disabled={saving || undefined}>
                    <FieldLabel htmlFor="admin-password">初始密码</FieldLabel>
                    <Input
                      id="admin-password"
                      type="password"
                      autoComplete="new-password"
                      required
                      minLength={8}
                      aria-invalid={passwordInvalid || undefined}
                      disabled={saving}
                      value={draft.password}
                      onChange={(event) => setDraft({ ...draft, password: event.target.value })}
                    />
                    <FieldDescription>至少 8 位；创建后请通过安全渠道转交。</FieldDescription>
                    {passwordInvalid && <FieldError>初始密码至少需要 8 位。</FieldError>}
                  </Field>
                )}

                <Field data-disabled={saving || undefined}>
                  <FieldLabel htmlFor="admin-role">角色</FieldLabel>
                  <Select
                    items={[
                      { value: 'operator', label: ROLE_LABEL.operator },
                      { value: 'super_admin', label: ROLE_LABEL.super_admin },
                    ]}
                    value={draft.role}
                    disabled={saving}
                    onValueChange={(value) => setDraft({ ...draft, role: value as AdminRole })}
                  >
                    <SelectTrigger id="admin-role" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="operator">{ROLE_LABEL.operator}</SelectItem>
                      <SelectItem value="super_admin">{ROLE_LABEL.super_admin}</SelectItem>
                    </SelectContent>
                  </Select>
                  <FieldDescription>
                    普通管理员只能管理用户；超级管理员可以管理其余全部内容与账号。
                  </FieldDescription>
                </Field>
              </FieldGroup>

              <DialogFooter>
                <Button type="button" variant="outline" disabled={saving} onClick={() => setDraft(null)}>
                  取消
                </Button>
                <Button type="submit" disabled={saving}>
                  {saving && <Spinner data-icon="inline-start" />}
                  {saving ? '保存中…' : '保存管理员'}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      <Dialog
        open={resetTarget !== null}
        onOpenChange={(open) => {
          if (!open && !resetBusy) setResetTarget(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>重置 {resetTarget?.username} 的密码</DialogTitle>
            <DialogDescription>
              新密码立即生效，该管理员所有已登录会话都会失效，需要重新登录。
            </DialogDescription>
          </DialogHeader>
          <Field data-invalid={resetInvalid || undefined} data-disabled={resetBusy || undefined}>
            <FieldLabel htmlFor="admin-new-password">新密码</FieldLabel>
            <Input
              id="admin-new-password"
              type="password"
              autoComplete="new-password"
              minLength={8}
              aria-invalid={resetInvalid || undefined}
              disabled={resetBusy}
              value={newPassword}
              onChange={(event) => setNewPassword(event.target.value)}
            />
            <FieldDescription>至少 8 位。</FieldDescription>
            {resetInvalid && <FieldError>新密码至少需要 8 位。</FieldError>}
          </Field>
          <DialogFooter>
            <Button type="button" variant="outline" disabled={resetBusy} onClick={() => setResetTarget(null)}>
              取消
            </Button>
            <Button type="button" disabled={resetBusy || newPassword.length < 8} onClick={() => void resetPassword()}>
              {resetBusy && <Spinner data-icon="inline-start" />}
              {resetBusy ? '重置中…' : '重置密码'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => !open && setPendingDelete(null)}
        title={`删除管理员「${pendingDelete?.username ?? ''}」？`}
        description="删除后无法恢复，该管理员的所有会话会立即失效。操作日志里的历史记录仍会保留。"
        confirmLabel="删除管理员"
        onConfirm={async () => {
          if (pendingDelete) await remove(pendingDelete)
        }}
      />
    </div>
  )
}

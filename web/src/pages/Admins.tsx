import { useCallback, useEffect, useState } from 'react'
import { EyeIcon, KeyRoundIcon, PencilIcon, PlusIcon, ShieldCheckIcon, ShieldIcon, Trash2Icon, TriangleAlertIcon } from 'lucide-react'
import { toast } from 'sonner'

import { type AdminAccount, type AdminRole, api } from '@/api'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { LoadErrorAlert, RefreshButton, TableSkeleton } from '@/components/load-state'
import { PageHeader } from '@/components/page-header'
import { PasswordInput } from '@/components/password-input'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
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
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldContent, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useDirtyDialog } from '@/hooks/use-unsaved-guard'
import { errorMessage, formatTime } from '@/lib/format'
import { useSession } from '@/lib/session'

type Draft = {
  id?: number
  username: string
  password: string
  role: AdminRole
  canViewConfig: boolean
}

const emptyDraft: Draft = { username: '', password: '', role: 'operator', canViewConfig: false }

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
  const [initialDraft, setInitialDraft] = useState<Draft | null>(null)
  const [showErrors, setShowErrors] = useState(false)
  const [saving, setSaving] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<AdminAccount | null>(null)
  const [resetTarget, setResetTarget] = useState<AdminAccount | null>(null)
  const [newPassword, setNewPassword] = useState('')
  const [resetBusy, setResetBusy] = useState(false)

  const closeDraft = useCallback(() => {
    setDraft(null)
    setInitialDraft(null)
    setShowErrors(false)
  }, [])
  const draftDirty = Boolean(draft && initialDraft && JSON.stringify(draft) !== JSON.stringify(initialDraft))
  const onDraftOpenChange = useDirtyDialog(draftDirty, saving, closeDraft)

  const closeReset = useCallback(() => {
    setResetTarget(null)
    setNewPassword('')
  }, [])
  const onResetOpenChange = useDirtyDialog(Boolean(resetTarget && newPassword.length > 0), resetBusy, closeReset)

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
    const next = { ...emptyDraft }
    setShowErrors(false)
    setInitialDraft(next)
    setDraft(next)
  }

  const openEdit = (admin: AdminAccount) => {
    const next: Draft = {
      id: admin.id,
      username: admin.username,
      password: '',
      role: admin.role,
      canViewConfig: admin.canViewConfig,
    }
    setShowErrors(false)
    setInitialDraft(next)
    setDraft(next)
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
        await api.put(`/admins/${draft.id}`, {
          username: draft.username.trim(),
          role: draft.role,
          canViewConfig: draft.canViewConfig,
        })
        toast.success('管理员已保存')
      } else {
        await api.post('/admins', {
          username: draft.username.trim(),
          password: draft.password,
          role: draft.role,
          canViewConfig: draft.canViewConfig,
        })
        toast.success('管理员已创建')
      }
      closeDraft()
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
      closeReset()
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
          <p>
            普通管理员默认只能管理用户（增删改用户、重置流量、重新下发、查看订阅）并只读查看概览与套餐；开启「只读查看配置」后，还可只读查看面板、入站、节点组、分流规则及订阅模板（入站敏感密钥会自动隐藏）。
          </p>
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
                        <div className="flex flex-wrap items-center gap-1.5">
                          <StatusBadge tone={admin.role === 'super_admin' ? 'good' : 'idle'}>
                            {ROLE_LABEL[admin.role] ?? admin.role}
                          </StatusBadge>
                          {admin.role === 'operator' && admin.canViewConfig && (
                            <Badge variant="outline">
                              <EyeIcon data-icon="inline-start" />
                              只读配置
                            </Badge>
                          )}
                        </div>
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
          修改自己的密码请到
          <a href="#/settings?tab=security" className="mx-1 underline underline-offset-4 hover:text-foreground">
            设置 → 账号安全
          </a>
          ；修改后所有会话都会失效。
        </CardFooter>
      </Card>

      <Dialog open={draft !== null} onOpenChange={onDraftOpenChange}>
        <DialogContent className="flex flex-col overflow-hidden">
          {draft && (
            <form className="contents" noValidate onSubmit={save}>
              <DialogHeader>
                <DialogTitle>{draft.id ? `编辑管理员 ${draft.username}` : '新建管理员'}</DialogTitle>
                <DialogDescription>
                  普通管理员只能管理用户和只读查看概览、套餐（可选开启配置只读）；超级管理员拥有全部权限。
                </DialogDescription>
              </DialogHeader>
              <DialogBody>
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
                      <PasswordInput
                        id="admin-password"
                        autoComplete="new-password"
                        required
                        minLength={8}
                        allowGenerate
                        allowCopy
                        aria-invalid={passwordInvalid || undefined}
                        disabled={saving}
                        value={draft.password}
                        onChangeValue={(password) => setDraft({ ...draft, password })}
                      />
                      <FieldDescription>至少 8 位；可点击骰子生成随机密码并复制转交。</FieldDescription>
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
                      普通管理员主要管理用户；超级管理员可以管理其余全部内容与账号。
                    </FieldDescription>
                  </Field>

                  {draft.role === 'operator' && (
                    <Field orientation="horizontal" data-disabled={saving || undefined}>
                      <FieldContent>
                        <FieldLabel htmlFor="admin-can-view-config">只读查看配置</FieldLabel>
                        <FieldDescription>
                          允许查看面板、入站、节点组、分流与订阅模板，但不能做任何修改。入站密钥会被隐藏。
                        </FieldDescription>
                      </FieldContent>
                      <Switch
                        id="admin-can-view-config"
                        checked={draft.canViewConfig}
                        disabled={saving}
                        onCheckedChange={(checked) => setDraft({ ...draft, canViewConfig: checked })}
                      />
                    </Field>
                  )}
                </FieldGroup>
              </DialogBody>

              <DialogFooter>
                <Button type="button" variant="outline" disabled={saving} onClick={() => onDraftOpenChange(false)}>
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

      <Dialog open={resetTarget !== null} onOpenChange={onResetOpenChange}>
        <DialogContent className="flex flex-col overflow-hidden">
          <DialogHeader>
            <DialogTitle>重置 {resetTarget?.username} 的密码</DialogTitle>
            <DialogDescription>
              新密码立即生效，该管理员所有已登录会话都会失效，需要重新登录。
            </DialogDescription>
          </DialogHeader>
          <DialogBody>
            <Field data-invalid={resetInvalid || undefined} data-disabled={resetBusy || undefined}>
              <FieldLabel htmlFor="admin-new-password">新密码</FieldLabel>
              <PasswordInput
                id="admin-new-password"
                autoComplete="new-password"
                minLength={8}
                allowGenerate
                allowCopy
                aria-invalid={resetInvalid || undefined}
                disabled={resetBusy}
                value={newPassword}
                onChangeValue={setNewPassword}
              />
              <FieldDescription>至少 8 位；可生成随机密码并复制给对方。</FieldDescription>
              {resetInvalid && <FieldError>新密码至少需要 8 位。</FieldError>}
            </Field>
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" disabled={resetBusy} onClick={() => onResetOpenChange(false)}>
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

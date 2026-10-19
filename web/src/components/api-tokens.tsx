import { type FormEvent, useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { CopyIcon, KeyRoundIcon, PlusIcon, Trash2Icon } from 'lucide-react'

import { type ApiToken, api } from '@/api'
import { CodeText } from '@/components/code-display'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { LoadErrorAlert } from '@/components/load-state'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { copyText, errorMessage, formatTime } from '@/lib/format'

interface Draft {
  name: string
  readOnly: boolean
  expiresInDays: number
}

const emptyDraft: Draft = { name: '', readOnly: false, expiresInDays: 0 }

/**
 * ApiTokens manages the bearer tokens scripts and agents use instead of a
 * browser session. The token itself is shown once, right after it is created.
 */
export function ApiTokens() {
  const [tokens, setTokens] = useState<ApiToken[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [attempted, setAttempted] = useState(false)
  const [creating, setCreating] = useState(false)
  const [minted, setMinted] = useState<string | null>(null)
  const [revoking, setRevoking] = useState<ApiToken | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setLoadError(null)
    try {
      setTokens(await api.get<ApiToken[]>('/tokens'))
    } catch (err) {
      setLoadError(errorMessage(err, '加载 API 令牌失败'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const nameInvalid = attempted && !draft?.name.trim()

  const create = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!draft) return
    setAttempted(true)
    if (!draft.name.trim()) return
    setCreating(true)
    try {
      const res = await api.post<{ token: string; apiToken: ApiToken }>('/tokens', draft)
      setDraft(null)
      setAttempted(false)
      setMinted(res.token)
      void load()
    } catch (err) {
      toast.error(errorMessage(err, '创建令牌失败'))
    } finally {
      setCreating(false)
    }
  }

  const revoke = async () => {
    if (!revoking) return
    try {
      await api.del(`/tokens/${revoking.id}`)
      toast.success(`已吊销「${revoking.name}」`)
      void load()
    } catch (err) {
      toast.error(errorMessage(err, '吊销令牌失败'))
      throw err
    }
  }

  const copy = async (text: string) => {
    if (await copyText(text)) toast.success('令牌已复制')
    else toast.error('复制失败，请手动选择复制')
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>API 令牌</CardTitle>
        <CardDescription>
          供脚本或本地 agent 调用管理 API，请求头带 <CodeText>Authorization: Bearer fxt_…</CodeText>。
          令牌拥有管理员权限，但不能管理令牌或修改密码。
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {loadError && (
          <LoadErrorAlert title="无法加载 API 令牌" message={loadError} retrying={loading} onRetry={() => void load()} />
        )}
        {tokens === null && !loadError ? (
          <Skeleton className="h-24 w-full" />
        ) : tokens && tokens.length === 0 ? (
          <p className="text-sm text-muted-foreground">还没有令牌。</p>
        ) : (
          tokens && (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>名称</TableHead>
                  <TableHead>权限</TableHead>
                  <TableHead className="hidden md:table-cell">最近使用</TableHead>
                  <TableHead className="hidden md:table-cell">过期</TableHead>
                  <TableHead><span className="sr-only">操作</span></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {tokens.map((token) => {
                  const expired = token.expiresAt > 0 && token.expiresAt <= Date.now()
                  return (
                    <TableRow key={token.id}>
                      <TableCell>
                        <div className="flex flex-col gap-1">
                          <span className="font-medium">{token.name}</span>
                          <span className="font-mono text-xs text-muted-foreground">{token.prefix}…</span>
                        </div>
                      </TableCell>
                      <TableCell>
                        {expired ? (
                          <StatusBadge tone="bad">已过期</StatusBadge>
                        ) : token.readOnly ? (
                          <StatusBadge tone="idle">只读</StatusBadge>
                        ) : (
                          <StatusBadge tone="warn">读写</StatusBadge>
                        )}
                      </TableCell>
                      <TableCell className="hidden text-muted-foreground md:table-cell">
                        {token.lastUsedAt ? formatTime(token.lastUsedAt) : '从未'}
                      </TableCell>
                      <TableCell className="hidden text-muted-foreground md:table-cell">
                        {token.expiresAt ? formatTime(token.expiresAt) : '永不'}
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end">
                          <Button variant="ghost" size="icon" aria-label={`吊销 ${token.name}`} onClick={() => setRevoking(token)}>
                            <Trash2Icon />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )
        )}
      </CardContent>
      <CardFooter>
        <Button className="w-full sm:w-auto" onClick={() => setDraft({ ...emptyDraft })}>
          <PlusIcon data-icon="inline-start" />
          新建令牌
        </Button>
      </CardFooter>

      <Dialog
        open={draft !== null}
        onOpenChange={(open) => {
          if (!open && !creating) {
            setDraft(null)
            setAttempted(false)
          }
        }}
      >
        <DialogContent>
          {draft && (
            <form className="contents" noValidate onSubmit={create}>
              <DialogHeader>
                <DialogTitle>新建 API 令牌</DialogTitle>
                <DialogDescription>令牌只在创建后显示一次，FireX 只保存它的哈希。</DialogDescription>
              </DialogHeader>
              <FieldGroup>
                <Field data-invalid={nameInvalid || undefined} data-disabled={creating || undefined}>
                  <FieldLabel htmlFor="token-name">名称</FieldLabel>
                  <Input
                    id="token-name"
                    placeholder="例如：本地 agent"
                    required
                    aria-invalid={nameInvalid || undefined}
                    disabled={creating}
                    value={draft.name}
                    onChange={(event) => setDraft({ ...draft, name: event.target.value })}
                  />
                  {nameInvalid && <FieldError>请输入令牌名称。</FieldError>}
                </Field>
                <Field data-disabled={creating || undefined}>
                  <FieldLabel htmlFor="token-expiry">有效期 (天)</FieldLabel>
                  <Input
                    id="token-expiry"
                    type="number"
                    min={0}
                    max={3650}
                    disabled={creating}
                    value={draft.expiresInDays}
                    onChange={(event) => setDraft({ ...draft, expiresInDays: Number(event.target.value) })}
                  />
                  <FieldDescription>0 表示永不过期。</FieldDescription>
                </Field>
                <Field orientation="horizontal" data-disabled={creating || undefined}>
                  <FieldLabel htmlFor="token-readonly">只读</FieldLabel>
                  <Switch
                    id="token-readonly"
                    disabled={creating}
                    checked={draft.readOnly}
                    onCheckedChange={(readOnly) => setDraft({ ...draft, readOnly })}
                  />
                </Field>
                <FieldDescription>只读令牌只能读取和预览快照，不能写入任何东西。</FieldDescription>
              </FieldGroup>
              <DialogFooter>
                <Button type="submit" disabled={creating}>
                  {creating ? <Spinner data-icon="inline-start" /> : <KeyRoundIcon data-icon="inline-start" />}
                  {creating ? '创建中…' : '创建'}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      <Dialog open={minted !== null} onOpenChange={(open) => !open && setMinted(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>令牌已创建</DialogTitle>
            <DialogDescription>现在复制保存。关闭后无法再次查看，丢失只能吊销重建。</DialogDescription>
          </DialogHeader>
          {minted && (
            <Alert>
              <KeyRoundIcon />
              <AlertTitle>API 令牌</AlertTitle>
              <AlertDescription>
                <code className="font-mono text-xs break-all select-all">{minted}</code>
              </AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <Button onClick={() => minted && void copy(minted)}>
              <CopyIcon data-icon="inline-start" />
              复制
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={revoking !== null}
        onOpenChange={(open) => !open && setRevoking(null)}
        title={`吊销「${revoking?.name ?? ''}」？`}
        description="使用这个令牌的脚本或 agent 会立即失去访问权限。"
        confirmLabel="吊销"
        onConfirm={revoke}
      />
    </Card>
  )
}

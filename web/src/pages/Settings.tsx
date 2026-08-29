import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { RotateCcwIcon, SaveIcon } from 'lucide-react'

import { api } from '@/api'
import { PageHeader } from '@/components/page-header'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import { errorMessage } from '@/lib/format'

interface TemplateResponse {
  template: string
  isDefault: boolean
  default: string
}

export function SettingsPage() {
  const [tpl, setTpl] = useState<TemplateResponse | null>(null)
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)

  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')

  const load = useCallback(async () => {
    const res = await api.get<TemplateResponse>('/settings/clashTemplate')
    setTpl(res)
    setText(res.template || res.default)
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const saveTemplate = async (value: string) => {
    setBusy(true)
    try {
      await api.put('/settings/clashTemplate', { template: value })
      toast.success('模板已保存')
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '保存失败'))
    } finally {
      setBusy(false)
    }
  }

  const changePassword = async () => {
    if (next.length < 8) {
      toast.error('新密码至少 8 位')
      return
    }
    try {
      await api.post('/auth/password', { current, new: next })
      toast.success('密码已修改，请重新登录')
      setTimeout(() => window.location.reload(), 1200)
    } catch (err) {
      toast.error(errorMessage(err, '修改失败'))
    }
  }

  if (!tpl) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="设置" description="Clash 订阅模板与管理员账号" />
        <Skeleton className="h-96 w-full" />
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="设置" description="Clash 订阅模板与管理员账号" />

      <Card>
        <CardHeader>
          <CardTitle>Clash 模板</CardTitle>
          <CardDescription>
            渲染时会替换 <code>proxies</code>，并展开 <code>proxy-groups</code> 里的占位符：
            <code>&lt;ALL&gt;</code> 全部节点、<code>&lt;REGION_GROUPS&gt;</code> 按地区自动分组、
            <code>&lt;REGION:名称&gt;</code>、<code>&lt;TAG:名称&gt;</code>、<code>&lt;FILTER:正则&gt;</code>。
            展开后为空的分组会被自动删除，指向它的规则会改写，避免客户端加载失败。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="clash-template">
                {tpl.isDefault ? '当前使用内置默认模板' : '自定义模板'}
              </FieldLabel>
              <Textarea
                id="clash-template"
                rows={22}
                spellCheck={false}
                className="font-mono text-xs"
                value={text}
                onChange={(e) => setText(e.target.value)}
              />
              <FieldDescription>保存前会先用探针节点试渲染，模板有问题会直接被拒绝。</FieldDescription>
            </Field>
          </FieldGroup>
        </CardContent>
        <CardFooter className="gap-2">
          <Button disabled={busy} onClick={() => saveTemplate(text)}>
            {busy ? <Spinner data-icon="inline-start" /> : <SaveIcon data-icon="inline-start" />}
            保存模板
          </Button>
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => {
              setText(tpl.default)
              void saveTemplate('')
            }}
          >
            <RotateCcwIcon data-icon="inline-start" />
            恢复默认
          </Button>
        </CardFooter>
      </Card>

      <Card className="max-w-md">
        <CardHeader>
          <CardTitle>修改密码</CardTitle>
          <CardDescription>修改后所有已登录的会话都会失效。</CardDescription>
        </CardHeader>
        <CardContent>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="pw-current">当前密码</FieldLabel>
              <Input
                id="pw-current"
                type="password"
                autoComplete="current-password"
                value={current}
                onChange={(e) => setCurrent(e.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="pw-new">新密码</FieldLabel>
              <Input
                id="pw-new"
                type="password"
                autoComplete="new-password"
                value={next}
                onChange={(e) => setNext(e.target.value)}
              />
              <FieldDescription>至少 8 位</FieldDescription>
            </Field>
          </FieldGroup>
        </CardContent>
        <CardFooter>
          <Button onClick={changePassword}>修改密码</Button>
        </CardFooter>
      </Card>
    </div>
  )
}

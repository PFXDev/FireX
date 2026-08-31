import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  ChevronDownIcon,
  EyeIcon,
  ListTreeIcon,
  PlusIcon,
  RotateCcwIcon,
  SaveIcon,
  SplitIcon,
  TrashIcon,
  TriangleAlertIcon,
} from 'lucide-react'
import { toast } from 'sonner'

import { api } from '@/api'
import type { NodeGroup, PolicyGroup, Routing, RoutingMember, RoutingMode, RoutingResponse, RoutingRule } from '@/api'
import { CodeBlock } from '@/components/code-display'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { PageHeader } from '@/components/page-header'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { errorMessage } from '@/lib/format'

/** Matchers where mihomo reads `no-resolve`; it is meaningless elsewhere. */
const NO_RESOLVE_TYPES = new Set(['GEOIP', 'IP-CIDR', 'IP-CIDR6', 'IP-SUFFIX', 'IP-ASN', 'SRC-IP-CIDR'])

const GROUP_TYPE_HINTS: Record<string, string> = {
  'select': '客户端手动选择',
  'url-test': '自动选延迟最低',
  'fallback': '按顺序故障转移',
  'load-balance': '负载均衡',
}

/** A member is addressed as `kind:ref` so it fits in a single <Select> value. */
function encodeMember(member: RoutingMember): string {
  return `${member.kind}:${member.ref}`
}

function decodeMember(value: string): RoutingMember {
  const at = value.indexOf(':')
  return { kind: value.slice(0, at) as RoutingMember['kind'], ref: value.slice(at + 1) }
}

function policyLabel(group: PolicyGroup): string {
  return group.icon ? `${group.icon} ${group.name}` : group.name
}

function groupLabel(group: NodeGroup): string {
  return group.emoji ? `${group.emoji} ${group.name}` : group.name
}

function move<T>(list: T[], index: number, delta: number): T[] {
  const target = index + delta
  if (target < 0 || target >= list.length) return list
  const next = [...list]
  const [item] = next.splice(index, 1)
  next.splice(target, 0, item)
  return next
}

export function RoutingPage() {
  const [data, setData] = useState<RoutingResponse | null>(null)
  const [groups, setGroups] = useState<NodeGroup[]>([])
  const [draft, setDraft] = useState<Routing | null>(null)
  const [mode, setMode] = useState<RoutingMode>('visual')
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [resetOpen, setResetOpen] = useState(false)
  const [preview, setPreview] = useState<{ yaml?: string; error?: string } | null>(null)
  const [previewing, setPreviewing] = useState(false)

  const load = useCallback(async () => {
    try {
      const [routing, nodeGroups] = await Promise.all([
        api.get<RoutingResponse>('/settings/routing'),
        api.get<NodeGroup[]>('/node-groups'),
      ])
      setData(routing)
      setGroups(nodeGroups)
      setDraft(structuredClone(routing.routing))
      setMode(routing.mode)
      setLoadError(null)
      return true
    } catch (err) {
      setLoadError(errorMessage(err, '分流配置加载失败'))
      return false
    }
  }, [])

  useEffect(() => {
    void load().finally(() => setLoading(false))
  }, [load])

  const enabledGroups = useMemo(() => groups.filter((group) => group.enabled), [groups])

  const save = async () => {
    if (!draft) return
    setSaving(true)
    try {
      await api.put('/settings/routing', { mode, routing: draft })
      toast.success('分流配置已保存')
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '保存失败'))
    } finally {
      setSaving(false)
    }
  }

  const restoreDefault = async () => {
    try {
      await api.post('/settings/routing/reset')
      toast.success('已恢复内置默认分流')
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '恢复默认失败'))
      throw err
    }
  }

  const runPreview = async () => {
    if (!draft) return
    setPreviewing(true)
    try {
      const result = await api.post<{ yaml?: string; error?: string }>('/settings/routing/preview', { routing: draft })
      setPreview(result)
    } catch (err) {
      setPreview({ error: errorMessage(err, '预览失败') })
    } finally {
      setPreviewing(false)
    }
  }

  // Policy groups are referenced by name, so a rename has to sweep every
  // member list and rule target with it.
  const renamePolicy = (index: number, name: string) => {
    if (!draft) return
    const previous = draft.groups[index].name
    const groupsNext = draft.groups.map((group, i) => {
      const renamed = i === index ? { ...group, name } : group
      return {
        ...renamed,
        members: renamed.members.map((member) =>
          member.kind === 'policy' && member.ref === previous ? { ...member, ref: name } : member,
        ),
      }
    })
    setDraft({
      groups: groupsNext,
      rules: draft.rules.map((rule) =>
        rule.target.kind === 'policy' && rule.target.ref === previous
          ? { ...rule, target: { ...rule.target, ref: name } }
          : rule,
      ),
      final:
        draft.final.kind === 'policy' && draft.final.ref === previous
          ? { ...draft.final, ref: name }
          : draft.final,
    })
  }

  const patchGroup = (index: number, patch: Partial<PolicyGroup>) => {
    if (!draft) return
    setDraft({ ...draft, groups: draft.groups.map((group, i) => (i === index ? { ...group, ...patch } : group)) })
  }

  const addGroup = () => {
    if (!draft) return
    let name = '新策略组'
    for (let i = 2; draft.groups.some((group) => group.name === name); i++) name = `新策略组 ${i}`
    setDraft({
      ...draft,
      groups: [
        ...draft.groups,
        { name, icon: '', type: 'select', members: [{ kind: 'builtin', ref: 'DIRECT' }], testUrl: '', interval: 300, tolerance: 50 },
      ],
    })
  }

  const removeGroup = (index: number) => {
    if (!draft) return
    const gone = draft.groups[index].name
    const rest = draft.groups.filter((_, i) => i !== index)
    const stripped = rest.map((group) => ({
      ...group,
      members: group.members.filter((member) => !(member.kind === 'policy' && member.ref === gone)),
    }))
    const fallback: RoutingMember = { kind: 'builtin', ref: 'DIRECT' }
    setDraft({
      groups: stripped,
      rules: draft.rules.filter((rule) => !(rule.target.kind === 'policy' && rule.target.ref === gone)),
      final: draft.final.kind === 'policy' && draft.final.ref === gone ? fallback : draft.final,
    })
  }

  const patchRule = (index: number, patch: Partial<RoutingRule>) => {
    if (!draft) return
    setDraft({ ...draft, rules: draft.rules.map((rule, i) => (i === index ? { ...rule, ...patch } : rule)) })
  }

  const addRule = () => {
    if (!draft) return
    const target: RoutingMember = draft.groups.length > 0
      ? { kind: 'policy', ref: draft.groups[0].name }
      : { kind: 'builtin', ref: 'DIRECT' }
    setDraft({
      ...draft,
      rules: [...draft.rules, { type: 'DOMAIN-SUFFIX', value: '', target, noResolve: false, disabled: false }],
    })
  }

  if (!data || !draft) {
    return (
      <div className="flex w-full max-w-6xl flex-col gap-6">
        <PageHeader title="分流" description="用可视化的策略组与规则决定流量走哪条线路。" />
        {loadError ? (
          <Alert variant="destructive">
            <TriangleAlertIcon />
            <AlertTitle>无法加载分流配置</AlertTitle>
            <AlertDescription className="flex flex-col items-start gap-3">
              <p>{loadError}</p>
              <Button variant="outline" size="sm" disabled={loading} onClick={() => void load()}>
                重新加载
              </Button>
            </AlertDescription>
          </Alert>
        ) : (
          <Skeleton className="h-96 w-full" />
        )}
      </div>
    )
  }

  const visual = mode === 'visual'
  const memberLabel = (member: RoutingMember): string => {
    switch (member.kind) {
      case 'policy': {
        const found = draft.groups.find((group) => group.name === member.ref)
        return found ? policyLabel(found) : `⚠️ 未知策略组 ${member.ref}`
      }
      case 'node-group': {
        const found = groups.find((group) => group.name === member.ref)
        return found ? groupLabel(found) : `⚠️ 未知分组 ${member.ref}`
      }
      case 'all-groups':
        return '全部节点分组'
      case 'all-nodes':
        return '全部节点'
      default:
        return member.ref
    }
  }

  return (
    <div className="flex w-full max-w-6xl flex-col gap-6">
      <PageHeader title="分流" description="用可视化的策略组与规则决定流量走哪条线路。">
        <Button variant="outline" disabled={saving} onClick={() => setResetOpen(true)}>
          <RotateCcwIcon data-icon="inline-start" />
          恢复默认
        </Button>
        <Button disabled={saving} onClick={() => void save()}>
          {saving ? <Spinner data-icon="inline-start" /> : <SaveIcon data-icon="inline-start" />}
          {saving ? '保存中…' : '保存分流'}
        </Button>
      </PageHeader>

      <Card>
        <CardHeader>
          <CardTitle>渲染模式</CardTitle>
          <CardDescription>
            可视化模式下，订阅的 <code>proxy-groups</code> 与 <code>rules</code> 由本页生成，
            设置里的 YAML 模板只负责 DNS、嗅探等基础配置。
          </CardDescription>
          <CardAction>
            <Field orientation="horizontal">
              <FieldLabel htmlFor="routing-mode">可视化分流</FieldLabel>
              <Switch
                id="routing-mode"
                checked={visual}
                onCheckedChange={(checked) => setMode(checked ? 'visual' : 'yaml')}
              />
            </Field>
          </CardAction>
        </CardHeader>
        {!visual && (
          <CardContent>
            <Alert variant="warning">
              <TriangleAlertIcon />
              <AlertTitle>当前由 YAML 模板接管</AlertTitle>
              <AlertDescription>
                订阅将使用「设置 → 订阅模板」里的 <code>proxy-groups</code> 和 <code>rules</code>，
                包括 <code>&lt;ALL&gt;</code>、<code>&lt;REGION_GROUPS&gt;</code> 等占位符。
                本页的编辑仍会保存，切回可视化即可生效。
              </AlertDescription>
            </Alert>
          </CardContent>
        )}
      </Card>

      {groups.length === 0 && (
        <Alert>
          <TriangleAlertIcon />
          <AlertTitle>还没有节点分组</AlertTitle>
          <AlertDescription>
            现在「全部节点分组」会退化为按节点地区自动分组。到「分组」页建好分组后，这里就能精确引用它们。
          </AlertDescription>
        </Alert>
      )}

      <Tabs defaultValue="groups" className="flex flex-col gap-4">
        <TabsList className="w-full sm:w-fit">
          <TabsTrigger value="groups">
            <ListTreeIcon />
            策略组 ({draft.groups.length})
          </TabsTrigger>
          <TabsTrigger value="rules">
            <SplitIcon />
            规则 ({draft.rules.length})
          </TabsTrigger>
          <TabsTrigger value="preview">
            <EyeIcon />
            预览
          </TabsTrigger>
        </TabsList>

        <TabsContent value="groups" className="flex flex-col gap-4">
          {draft.groups.map((group, index) => (
            // Keyed by position, not name: a name-based key would remount the
            // card on every keystroke and drop focus out of the rename field.
            <Card key={index}>
              <CardHeader>
                <CardTitle className="flex flex-wrap items-center gap-2">
                  <Input
                    aria-label={`策略组 ${group.name} 的图标`}
                    className="w-16"
                    placeholder="🚀"
                    value={group.icon}
                    onChange={(event) => patchGroup(index, { icon: event.target.value })}
                  />
                  <Input
                    aria-label={`策略组 ${group.name} 的名称`}
                    className="w-48"
                    value={group.name}
                    onChange={(event) => renamePolicy(index, event.target.value)}
                  />
                  <Select
                    items={data.options.groupTypes.map((value) => ({ value, label: value }))}
                    value={group.type}
                    onValueChange={(value) => patchGroup(index, { type: String(value) })}
                  >
                    <SelectTrigger aria-label={`策略组 ${group.name} 的类型`}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {data.options.groupTypes.map((value) => (
                          <SelectItem key={value} value={value}>
                            {value}
                            <span className="text-muted-foreground">{GROUP_TYPE_HINTS[value]}</span>
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </CardTitle>
                <CardAction className="flex items-center gap-1">
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={index === 0}
                    aria-label={`上移 ${group.name}`}
                    onClick={() => setDraft({ ...draft, groups: move(draft.groups, index, -1) })}
                  >
                    上移
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={index === draft.groups.length - 1}
                    aria-label={`下移 ${group.name}`}
                    onClick={() => setDraft({ ...draft, groups: move(draft.groups, index, 1) })}
                  >
                    下移
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`删除策略组 ${group.name}`}
                    onClick={() => removeGroup(index)}
                  >
                    <TrashIcon />
                  </Button>
                </CardAction>
              </CardHeader>
              <CardContent className="flex flex-col gap-3">
                <div className="flex flex-wrap items-center gap-2">
                  {group.members.map((member, memberIndex) => (
                    <Badge key={`${encodeMember(member)}-${memberIndex}`} variant="secondary" className="gap-1 pr-1">
                      {memberLabel(member)}
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        className="size-5"
                        aria-label={`把 ${memberLabel(member)} 前移`}
                        disabled={memberIndex === 0}
                        onClick={() => patchGroup(index, { members: move(group.members, memberIndex, -1) })}
                      >
                        ←
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        className="size-5"
                        aria-label={`移除成员 ${memberLabel(member)}`}
                        onClick={() =>
                          patchGroup(index, { members: group.members.filter((_, i) => i !== memberIndex) })
                        }
                      >
                        ×
                      </Button>
                    </Badge>
                  ))}
                  <MemberPicker
                    label="添加成员"
                    policies={draft.groups.filter((candidate) => candidate.name !== group.name)}
                    groups={enabledGroups}
                    builtins={data.options.builtins}
                    allowExpansions
                    onPick={(member) => patchGroup(index, { members: [...group.members, member] })}
                  />
                </div>
                {group.members.length === 0 && (
                  <p className="text-sm text-destructive">策略组至少需要一个成员，否则无法保存。</p>
                )}
                {group.type !== 'select' && (
                  <div className="flex flex-wrap items-end gap-3">
                    <Field className="w-72">
                      <FieldLabel htmlFor={`policy-url-${index}`}>测速地址</FieldLabel>
                      <Input
                        id={`policy-url-${index}`}
                        placeholder="https://www.gstatic.com/generate_204"
                        value={group.testUrl}
                        onChange={(event) => patchGroup(index, { testUrl: event.target.value })}
                      />
                    </Field>
                    <Field className="w-32">
                      <FieldLabel htmlFor={`policy-interval-${index}`}>间隔 (秒)</FieldLabel>
                      <Input
                        id={`policy-interval-${index}`}
                        type="number"
                        min={0}
                        value={group.interval}
                        onChange={(event) => patchGroup(index, { interval: Number(event.target.value) })}
                      />
                    </Field>
                    {group.type === 'url-test' && (
                      <Field className="w-32">
                        <FieldLabel htmlFor={`policy-tolerance-${index}`}>容差 (毫秒)</FieldLabel>
                        <Input
                          id={`policy-tolerance-${index}`}
                          type="number"
                          min={0}
                          value={group.tolerance}
                          onChange={(event) => patchGroup(index, { tolerance: Number(event.target.value) })}
                        />
                      </Field>
                    )}
                  </div>
                )}
              </CardContent>
            </Card>
          ))}
          <div>
            <Button variant="outline" onClick={addGroup}>
              <PlusIcon data-icon="inline-start" />
              新建策略组
            </Button>
          </div>
        </TabsContent>

        <TabsContent value="rules" className="flex flex-col gap-4">
          <Card>
            <CardHeader>
              <CardTitle>规则顺序</CardTitle>
              <CardDescription>自上而下匹配，命中即停止。都不命中时走下方的兜底策略。</CardDescription>
              <CardAction>
                <Button variant="outline" size="sm" onClick={addRule}>
                  <PlusIcon data-icon="inline-start" />
                  添加规则
                </Button>
              </CardAction>
            </CardHeader>
            <CardContent className="px-0">
              {draft.rules.length === 0 ? (
                <Empty>
                  <EmptyHeader>
                    <EmptyMedia variant="icon">
                      <SplitIcon />
                    </EmptyMedia>
                    <EmptyTitle>还没有规则</EmptyTitle>
                    <EmptyDescription>所有流量都会走兜底策略。</EmptyDescription>
                  </EmptyHeader>
                  <EmptyContent>
                    <Button onClick={addRule}>
                      <PlusIcon data-icon="inline-start" />
                      添加规则
                    </Button>
                  </EmptyContent>
                </Empty>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-40">匹配类型</TableHead>
                      <TableHead>内容</TableHead>
                      <TableHead className="w-56">目标</TableHead>
                      <TableHead className="hidden w-28 lg:table-cell">no-resolve</TableHead>
                      <TableHead className="w-40">
                        <span className="sr-only">顺序与操作</span>
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {draft.rules.map((rule, index) => (
                      <TableRow key={index} data-disabled={rule.disabled || undefined}>
                        <TableCell>
                          <Select
                            items={data.options.ruleTypes.map((value) => ({ value, label: value }))}
                            value={rule.type}
                            onValueChange={(value) => patchRule(index, { type: String(value) })}
                          >
                            <SelectTrigger className="w-full" aria-label={`规则 ${index + 1} 的匹配类型`}>
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectGroup>
                                {data.options.ruleTypes.map((value) => (
                                  <SelectItem key={value} value={value}>
                                    {value}
                                  </SelectItem>
                                ))}
                              </SelectGroup>
                            </SelectContent>
                          </Select>
                        </TableCell>
                        <TableCell>
                          <Input
                            aria-label={`规则 ${index + 1} 的内容`}
                            placeholder="例如 openai.com"
                            aria-invalid={!rule.value.trim() || undefined}
                            value={rule.value}
                            onChange={(event) => patchRule(index, { value: event.target.value })}
                          />
                        </TableCell>
                        <TableCell>
                          <MemberSelect
                            label={`规则 ${index + 1} 的目标`}
                            value={rule.target}
                            policies={draft.groups}
                            groups={enabledGroups}
                            builtins={data.options.builtins}
                            onChange={(target) => patchRule(index, { target })}
                          />
                        </TableCell>
                        <TableCell className="hidden lg:table-cell">
                          <Checkbox
                            aria-label={`规则 ${index + 1} 不解析域名`}
                            disabled={!NO_RESOLVE_TYPES.has(rule.type)}
                            checked={rule.noResolve && NO_RESOLVE_TYPES.has(rule.type)}
                            onCheckedChange={(checked) => patchRule(index, { noResolve: Boolean(checked) })}
                          />
                        </TableCell>
                        <TableCell>
                          <div className="flex items-center justify-end gap-1">
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              aria-label={`上移规则 ${index + 1}`}
                              disabled={index === 0}
                              onClick={() => setDraft({ ...draft, rules: move(draft.rules, index, -1) })}
                            >
                              ↑
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              aria-label={`下移规则 ${index + 1}`}
                              disabled={index === draft.rules.length - 1}
                              onClick={() => setDraft({ ...draft, rules: move(draft.rules, index, 1) })}
                            >
                              ↓
                            </Button>
                            <Switch
                              aria-label={`启用规则 ${index + 1}`}
                              checked={!rule.disabled}
                              onCheckedChange={(checked) => patchRule(index, { disabled: !checked })}
                            />
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              aria-label={`删除规则 ${index + 1}`}
                              onClick={() => setDraft({ ...draft, rules: draft.rules.filter((_, i) => i !== index) })}
                            >
                              <TrashIcon />
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>兜底策略</CardTitle>
              <CardDescription>对应 mihomo 的 MATCH 规则，永远是最后一条。</CardDescription>
            </CardHeader>
            <CardContent>
              <Field className="max-w-sm">
                <FieldLabel htmlFor="routing-final">未命中任何规则时</FieldLabel>
                <MemberSelect
                  label="兜底策略"
                  id="routing-final"
                  value={draft.final}
                  policies={draft.groups}
                  groups={enabledGroups}
                  builtins={data.options.builtins}
                  onChange={(final) => setDraft({ ...draft, final })}
                />
                <FieldDescription>选一个包含节点的策略组，避免漏网流量直连。</FieldDescription>
              </Field>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="preview" className="flex flex-col gap-4">
          <Card>
            <CardHeader>
              <CardTitle>渲染预览</CardTitle>
              <CardDescription>
                按当前编辑内容渲染一份完整配置，节点取所有已启用节点（代理字段是占位，实际值在订阅时从面板取回）。
              </CardDescription>
              <CardAction>
                <Button variant="outline" size="sm" disabled={previewing} onClick={() => void runPreview()}>
                  {previewing ? <Spinner data-icon="inline-start" /> : <EyeIcon data-icon="inline-start" />}
                  生成预览
                </Button>
              </CardAction>
            </CardHeader>
            <CardContent>
              {preview?.error && (
                <Alert variant="destructive">
                  <TriangleAlertIcon />
                  <AlertTitle>配置有问题</AlertTitle>
                  <AlertDescription>{preview.error}</AlertDescription>
                </Alert>
              )}
              {preview?.yaml && <CodeBlock className="max-h-[60vh]">{preview.yaml}</CodeBlock>}
              {!preview && <p className="text-sm text-muted-foreground">还没有生成预览。</p>}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      <ConfirmDialog
        open={resetOpen}
        onOpenChange={setResetOpen}
        title="恢复内置默认分流？"
        description="当前的策略组与规则会被内置默认配置替换，未保存的编辑也会丢失。"
        confirmLabel="恢复默认"
        onConfirm={restoreDefault}
      />
    </div>
  )
}

/** Shared option set for the two ways of choosing a member. */
type MemberOptions = {
  policies: PolicyGroup[]
  groups: NodeGroup[]
  builtins: string[]
}

function MemberPicker({
  label,
  policies,
  groups,
  builtins,
  allowExpansions,
  onPick,
}: MemberOptions & {
  label: string
  allowExpansions?: boolean
  onPick: (member: RoutingMember) => void
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="outline" size="sm" />}>
        <PlusIcon data-icon="inline-start" />
        {label}
        <ChevronDownIcon data-icon="inline-end" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="max-h-80 min-w-56 overflow-y-auto">
        {allowExpansions && (
          <>
            <DropdownMenuGroup>
              <DropdownMenuLabel>动态展开</DropdownMenuLabel>
              <DropdownMenuItem onClick={() => onPick({ kind: 'all-groups', ref: '' })}>
                全部节点分组
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => onPick({ kind: 'all-nodes', ref: '' })}>全部节点</DropdownMenuItem>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
          </>
        )}
        {groups.length > 0 && (
          <>
            <DropdownMenuGroup>
              <DropdownMenuLabel>节点分组</DropdownMenuLabel>
              {groups.map((group) => (
                <DropdownMenuItem key={group.id} onClick={() => onPick({ kind: 'node-group', ref: group.name })}>
                  {groupLabel(group)}
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
          </>
        )}
        {policies.length > 0 && (
          <>
            <DropdownMenuGroup>
              <DropdownMenuLabel>策略组</DropdownMenuLabel>
              {policies.map((policy) => (
                <DropdownMenuItem key={policy.name} onClick={() => onPick({ kind: 'policy', ref: policy.name })}>
                  {policyLabel(policy)}
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
          </>
        )}
        <DropdownMenuGroup>
          <DropdownMenuLabel>内置策略</DropdownMenuLabel>
          {builtins.map((builtin) => (
            <DropdownMenuItem key={builtin} onClick={() => onPick({ kind: 'builtin', ref: builtin })}>
              {builtin}
            </DropdownMenuItem>
          ))}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function MemberSelect({
  id,
  label,
  value,
  policies,
  groups,
  builtins,
  onChange,
}: MemberOptions & {
  id?: string
  label: string
  value: RoutingMember
  onChange: (member: RoutingMember) => void
}) {
  const items = [
    ...policies.map((policy) => ({ value: `policy:${policy.name}`, label: policyLabel(policy) })),
    ...groups.map((group) => ({ value: `node-group:${group.name}`, label: groupLabel(group) })),
    ...builtins.map((builtin) => ({ value: `builtin:${builtin}`, label: builtin })),
  ]

  return (
    <Select items={items} value={encodeMember(value)} onValueChange={(next) => onChange(decodeMember(String(next)))}>
      <SelectTrigger id={id} className="w-full" aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent className="max-h-80">
        {policies.length > 0 && (
          <SelectGroup>
            <SelectLabel>策略组</SelectLabel>
            {policies.map((policy) => (
              <SelectItem key={policy.name} value={`policy:${policy.name}`}>
                {policyLabel(policy)}
              </SelectItem>
            ))}
          </SelectGroup>
        )}
        {groups.length > 0 && (
          <SelectGroup>
            <SelectLabel>节点分组</SelectLabel>
            {groups.map((group) => (
              <SelectItem key={group.id} value={`node-group:${group.name}`}>
                {groupLabel(group)}
              </SelectItem>
            ))}
          </SelectGroup>
        )}
        <SelectGroup>
          <SelectLabel>内置策略</SelectLabel>
          {builtins.map((builtin) => (
            <SelectItem key={builtin} value={`builtin:${builtin}`}>
              {builtin}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}

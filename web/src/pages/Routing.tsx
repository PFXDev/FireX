import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  ChevronDownIcon,
  EyeIcon,
  ListTreeIcon,
  PlusIcon,
  SaveIcon,
  TrashIcon,
  TriangleAlertIcon,
} from 'lucide-react'
import { toast } from 'sonner'

import { api } from '@/api'
import type { Egress, EgressMember, MemberKind, NodeGroup, Policy, Profile, RoutingMatrix } from '@/api'
import { CodeBlock } from '@/components/code-display'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { PageHeader } from '@/components/page-header'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldGroup, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { errorMessage } from '@/lib/format'

/** profileId 0 addresses the default column every profile falls back to. */
const DEFAULT_COLUMN = 0

/** One matrix row: a policy plus the egress it takes in each column. */
type Row = {
  policy: Policy
  cells: Map<number, Egress>
}

type ProfileDraft = {
  id?: number
  name: string
  allGroups: boolean
  enabled: boolean
  remark: string
  groupIds: number[]
}

function blankEgress(profileId: number): Egress {
  return {
    policyIndex: 0,
    profileId,
    type: 'select',
    testUrl: '',
    interval: 300,
    tolerance: 50,
    hidden: false,
    members: [],
  }
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
  const [rows, setRows] = useState<Row[]>([])
  const [options, setOptions] = useState<RoutingMatrix['options'] | null>(null)
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [groups, setGroups] = useState<NodeGroup[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [dirty, setDirty] = useState(false)

  const [policyEditor, setPolicyEditor] = useState<number | null>(null)
  const [cellEditor, setCellEditor] = useState<{ row: number; profileId: number } | null>(null)
  const [profileDraft, setProfileDraft] = useState<ProfileDraft | null>(null)
  const [pendingProfileDelete, setPendingProfileDelete] = useState<Profile | null>(null)
  const [pendingPolicyDelete, setPendingPolicyDelete] = useState<number | null>(null)

  const [previewProfile, setPreviewProfile] = useState<number>(DEFAULT_COLUMN)
  const [preview, setPreview] = useState<{ yaml?: string; error?: string } | null>(null)
  const [previewing, setPreviewing] = useState(false)

  const load = useCallback(async () => {
    try {
      const [matrix, nextProfiles, nextGroups] = await Promise.all([
        api.get<RoutingMatrix>('/routing'),
        api.get<Profile[]>('/profiles'),
        api.get<NodeGroup[]>('/node-groups'),
      ])
      const byIndex = new Map<number, Map<number, Egress>>()
      matrix.egresses.forEach((egress) => {
        if (!byIndex.has(egress.policyIndex)) byIndex.set(egress.policyIndex, new Map())
        byIndex.get(egress.policyIndex)!.set(egress.profileId, egress)
      })
      setRows(matrix.policies.map((policy, index) => ({ policy, cells: byIndex.get(index) ?? new Map() })))
      setOptions(matrix.options)
      setProfiles(nextProfiles)
      setGroups(nextGroups)
      setLoadError(null)
      setDirty(false)
      return true
    } catch (err) {
      setLoadError(errorMessage(err, '分流配置加载失败'))
      return false
    }
  }, [])

  useEffect(() => {
    void load().finally(() => setLoading(false))
  }, [load])

  const patchRows = (next: Row[]) => {
    setRows(next)
    setDirty(true)
  }

  const save = async () => {
    setSaving(true)
    try {
      await api.put('/routing', {
        policies: rows.map((row) => row.policy),
        egresses: rows.flatMap((row, index) =>
          [...row.cells.values()].map((egress) => ({ ...egress, policyIndex: index })),
        ),
      })
      toast.success('分流已保存')
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '保存失败'))
    } finally {
      setSaving(false)
    }
  }

  const runPreview = async () => {
    setPreviewing(true)
    try {
      setPreview(await api.get<{ yaml?: string; error?: string }>(`/routing/preview?profileId=${previewProfile}`))
    } catch (err) {
      setPreview({ error: errorMessage(err, '预览失败') })
    } finally {
      setPreviewing(false)
    }
  }

  const groupByName = useMemo(() => new Map(groups.map((group) => [group.name, group])), [groups])

  const memberLabel = useCallback(
    (member: EgressMember): string => {
      switch (member.kind) {
        case 'node-group': {
          const found = groupByName.get(member.ref)
          return found ? (found.emoji ? `${found.emoji} ${found.name}` : found.name) : `⚠️ ${member.ref}`
        }
        case 'policy': {
          const found = rows.find((row) => row.policy.name === member.ref)
          return found ? (found.policy.icon ? `${found.policy.icon} ${found.policy.name}` : found.policy.name) : `⚠️ ${member.ref}`
        }
        case 'all-node-groups':
          return '全部节点组'
        case 'all-inbounds':
          return '全部入站'
        default:
          return member.ref
      }
    },
    [groupByName, rows],
  )

  /** The egress that actually applies to a column, default included. */
  const effective = (row: Row, profileId: number): { egress: Egress; inherited: boolean } | null => {
    const own = row.cells.get(profileId)
    if (own) return { egress: own, inherited: false }
    const fallback = row.cells.get(DEFAULT_COLUMN)
    if (fallback) return { egress: fallback, inherited: true }
    return null
  }

  const setCell = (rowIndex: number, profileId: number, egress: Egress | null) => {
    const next = rows.map((row, i) => {
      if (i !== rowIndex) return row
      const cells = new Map(row.cells)
      if (egress) cells.set(profileId, egress)
      else cells.delete(profileId)
      return { ...row, cells }
    })
    patchRows(next)
  }

  const addPolicy = () => {
    let name = '新策略'
    for (let i = 2; rows.some((row) => row.policy.name === name); i++) name = `新策略 ${i}`
    const cells = new Map<number, Egress>()
    cells.set(DEFAULT_COLUMN, { ...blankEgress(DEFAULT_COLUMN), members: [{ kind: 'all-node-groups', ref: '' }] })
    patchRows([
      ...rows,
      { policy: { id: 0, name, icon: '', isFinal: false, enabled: true, remark: '', rules: [] }, cells },
    ])
    setPolicyEditor(rows.length)
  }

  const removePolicy = (index: number) => {
    const gone = rows[index].policy.name
    const stripped = rows
      .filter((_, i) => i !== index)
      .map((row) => ({
        ...row,
        cells: new Map(
          [...row.cells.entries()].map(([profileId, egress]) => [
            profileId,
            { ...egress, members: egress.members.filter((m) => !(m.kind === 'policy' && m.ref === gone)) },
          ]),
        ),
      }))
    patchRows(stripped)
  }

  const saveProfile = async () => {
    if (!profileDraft) return
    try {
      if (profileDraft.id) {
        await api.put(`/profiles/${profileDraft.id}`, profileDraft)
        toast.success('方案已保存，相关用户已同步到面板')
      } else {
        await api.post('/profiles', profileDraft)
        toast.success('方案已创建')
      }
      setProfileDraft(null)
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '保存失败'))
    }
  }

  const removeProfile = async (profile: Profile) => {
    try {
      await api.del(`/profiles/${profile.id}`)
      toast.success('方案已删除')
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '删除失败'))
      throw err
    }
  }

  if (loading || !options) {
    return (
      <div className="flex w-full flex-col gap-6">
        <PageHeader title="分流" description="行是分流策略，列是分流方案，格子里是出口。" />
        {loadError ? (
          <Alert variant="destructive">
            <TriangleAlertIcon />
            <AlertTitle>无法加载分流配置</AlertTitle>
            <AlertDescription className="flex flex-col items-start gap-3">
              <p>{loadError}</p>
              <Button variant="outline" size="sm" onClick={() => void load()}>
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

  const editingRow = policyEditor !== null ? rows[policyEditor] : null
  const editingCell = cellEditor ? effective(rows[cellEditor.row], cellEditor.profileId) : null

  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader
        title="分流"
        description="行是分流策略（一份规则清单），列是分流方案。第一列是默认出口，方案列只写和默认不同的部分。"
      >
        <Button variant="outline" onClick={addPolicy}>
          <PlusIcon data-icon="inline-start" />
          新建策略
        </Button>
        <Button disabled={saving || !dirty} onClick={() => void save()}>
          {saving ? <Spinner data-icon="inline-start" /> : <SaveIcon data-icon="inline-start" />}
          {saving ? '保存中…' : dirty ? '保存分流' : '已保存'}
        </Button>
      </PageHeader>

      <Card>
        <CardHeader>
          <CardTitle>分流方案</CardTitle>
          <CardDescription>
            方案的「可用节点组」决定它的用户能连上哪些入站——这是唯一会写到面板的设置。套餐绑方案，用户绑套餐。
          </CardDescription>
          <CardAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() =>
                setProfileDraft({ name: '', allGroups: false, enabled: true, remark: '', groupIds: [] })
              }
            >
              <PlusIcon data-icon="inline-start" />
              新建方案
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent className="px-0">
          {profiles.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ListTreeIcon />
                </EmptyMedia>
                <EmptyTitle>还没有分流方案</EmptyTitle>
                <EmptyDescription>没有方案，套餐就无处可绑，用户也拿不到任何节点。</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>方案</TableHead>
                  <TableHead>可用节点组</TableHead>
                  <TableHead className="hidden md:table-cell">可用入站</TableHead>
                  <TableHead className="hidden md:table-cell">套餐</TableHead>
                  <TableHead>
                    <span className="sr-only">操作</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {profiles.map((profile) => (
                  <TableRow key={profile.id}>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <strong>{profile.name}</strong>
                        {!profile.enabled && <StatusBadge tone="idle">停用</StatusBadge>}
                      </div>
                      {profile.remark && <span className="text-muted-foreground">{profile.remark}</span>}
                    </TableCell>
                    <TableCell>
                      {profile.allGroups ? (
                        <Badge variant="secondary">全部节点组</Badge>
                      ) : profile.groupIds.length === 0 ? (
                        <StatusBadge tone="warn">未选任何分组</StatusBadge>
                      ) : (
                        <span className="tabular-nums">{profile.groupIds.length}</span>
                      )}
                    </TableCell>
                    <TableCell className="hidden tabular-nums md:table-cell">{profile.usableInbounds}</TableCell>
                    <TableCell className="hidden tabular-nums md:table-cell">{profile.planCount}</TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() =>
                            setProfileDraft({
                              id: profile.id,
                              name: profile.name,
                              allGroups: profile.allGroups,
                              enabled: profile.enabled,
                              remark: profile.remark,
                              groupIds: profile.groupIds,
                            })
                          }
                        >
                          编辑
                        </Button>
                        <Button
                          variant="destructive"
                          size="sm"
                          aria-label={`删除方案 ${profile.name}`}
                          onClick={() => setPendingProfileDelete(profile)}
                        >
                          删除
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
          <CardTitle>分流矩阵</CardTitle>
          <CardDescription>
            行的先后既是规则的匹配顺序，也是客户端里策略组的排列顺序。点行首编规则清单，点格子改出口。
          </CardDescription>
          {dirty && (
            <CardAction>
              <Badge variant="secondary">有未保存的改动</Badge>
            </CardAction>
          )}
        </CardHeader>
        <CardContent className="px-0">
          <ScrollArea className="w-full">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="min-w-56">分流策略</TableHead>
                  <TableHead className="min-w-48">默认出口</TableHead>
                  {profiles.map((profile) => (
                    <TableHead key={profile.id} className="min-w-44">
                      {profile.name}
                    </TableHead>
                  ))}
                  <TableHead className="w-28">
                    <span className="sr-only">顺序与操作</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((row, index) => (
                  <TableRow key={index} data-disabled={!row.policy.enabled || undefined}>
                    <TableCell>
                      <button
                        type="button"
                        className="flex flex-col items-start gap-1 text-left"
                        onClick={() => setPolicyEditor(index)}
                      >
                        <span className="flex items-center gap-2 font-medium">
                          {row.policy.icon && <span>{row.policy.icon}</span>}
                          {row.policy.name}
                          {row.policy.isFinal && <Badge variant="outline">兜底</Badge>}
                          {!row.policy.enabled && <StatusBadge tone="idle">停用</StatusBadge>}
                        </span>
                        <span className="text-xs text-muted-foreground">
                          {row.policy.rules.length === 0 ? '无规则（纯选择器）' : `${row.policy.rules.length} 条规则`}
                        </span>
                      </button>
                    </TableCell>

                    <CellButton
                      summary={cellSummary(effective(row, DEFAULT_COLUMN), memberLabel, false)}
                      onClick={() => setCellEditor({ row: index, profileId: DEFAULT_COLUMN })}
                    />

                    {profiles.map((profile) => (
                      <CellButton
                        key={profile.id}
                        summary={cellSummary(effective(row, profile.id), memberLabel, !row.cells.has(profile.id))}
                        onClick={() => setCellEditor({ row: index, profileId: profile.id })}
                      />
                    ))}

                    <TableCell>
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`上移 ${row.policy.name}`}
                          disabled={index === 0}
                          onClick={() => patchRows(move(rows, index, -1))}
                        >
                          ↑
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`下移 ${row.policy.name}`}
                          disabled={index === rows.length - 1}
                          onClick={() => patchRows(move(rows, index, 1))}
                        >
                          ↓
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`删除策略 ${row.policy.name}`}
                          onClick={() => setPendingPolicyDelete(index)}
                        >
                          <TrashIcon />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </ScrollArea>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>渲染预览</CardTitle>
          <CardDescription>
            按已保存的配置渲染一份完整配置。代理字段是占位，实际值在订阅时从面板取回，但分组和规则就是客户端会拿到的。
          </CardDescription>
          <CardAction className="flex items-center gap-2">
            <Select
              items={[
                { value: String(DEFAULT_COLUMN), label: '默认（不含任何节点组）' },
                ...profiles.map((p) => ({ value: String(p.id), label: p.name })),
              ]}
              value={String(previewProfile)}
              onValueChange={(value) => setPreviewProfile(Number(value))}
            >
              <SelectTrigger aria-label="预览哪个方案" className="w-56">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value={String(DEFAULT_COLUMN)}>默认（不含任何节点组）</SelectItem>
                  {profiles.map((profile) => (
                    <SelectItem key={profile.id} value={String(profile.id)}>
                      {profile.name}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <Button variant="outline" size="sm" disabled={previewing} onClick={() => void runPreview()}>
              {previewing ? <Spinner data-icon="inline-start" /> : <EyeIcon data-icon="inline-start" />}
              生成预览
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent>
          {dirty && (
            <Alert variant="warning" className="mb-3">
              <TriangleAlertIcon />
              <AlertTitle>预览用的是已保存的配置</AlertTitle>
              <AlertDescription>当前还有未保存的改动，先保存再预览才能看到它们。</AlertDescription>
            </Alert>
          )}
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

      <PolicyDialog
        row={editingRow}
        options={options}
        onClose={() => setPolicyEditor(null)}
        onChange={(policy) => {
          if (policyEditor === null) return
          patchRows(rows.map((row, i) => (i === policyEditor ? { ...row, policy } : row)))
        }}
        onSetFinal={() => {
          if (policyEditor === null) return
          patchRows(
            rows.map((row, i) => ({ ...row, policy: { ...row.policy, isFinal: i === policyEditor } })),
          )
        }}
      />

      <EgressDialog
        open={cellEditor !== null}
        profileName={
          cellEditor?.profileId === DEFAULT_COLUMN
            ? '默认'
            : (profiles.find((p) => p.id === cellEditor?.profileId)?.name ?? '')
        }
        policyName={cellEditor ? rows[cellEditor.row].policy.name : ''}
        isDefaultColumn={cellEditor?.profileId === DEFAULT_COLUMN}
        own={cellEditor ? (rows[cellEditor.row].cells.get(cellEditor.profileId) ?? null) : null}
        inherited={editingCell?.inherited ? editingCell.egress : null}
        options={options}
        groups={groups}
        policies={rows.map((row) => row.policy)}
        memberLabel={memberLabel}
        onClose={() => setCellEditor(null)}
        onChange={(egress) => {
          if (!cellEditor) return
          setCell(cellEditor.row, cellEditor.profileId, egress)
        }}
      />

      <ProfileDialog
        draft={profileDraft}
        groups={groups}
        onChange={setProfileDraft}
        onClose={() => setProfileDraft(null)}
        onSave={() => void saveProfile()}
      />

      <ConfirmDialog
        open={pendingProfileDelete !== null}
        onOpenChange={(open) => !open && setPendingProfileDelete(null)}
        title={`删除方案「${pendingProfileDelete?.name ?? ''}」？`}
        description="节点组和策略都不受影响，但这个方案那一列的出口覆盖会一并删除。绑定了它的套餐必须先改掉。"
        confirmLabel="删除方案"
        onConfirm={async () => {
          if (pendingProfileDelete) await removeProfile(pendingProfileDelete)
        }}
      />

      <ConfirmDialog
        open={pendingPolicyDelete !== null}
        onOpenChange={(open) => !open && setPendingPolicyDelete(null)}
        title={`删除策略「${pendingPolicyDelete !== null ? rows[pendingPolicyDelete]?.policy.name : ''}」？`}
        description="它的规则清单、每一列的出口，以及其他策略里对它的引用都会一并移除。保存后才会真正生效。"
        confirmLabel="删除策略"
        onConfirm={async () => {
          if (pendingPolicyDelete !== null) removePolicy(pendingPolicyDelete)
          setPendingPolicyDelete(null)
        }}
      />
    </div>
  )
}

/** cellSummary renders what a column actually does, inheritance included. */
function cellSummary(
  resolved: { egress: Egress; inherited: boolean } | null,
  memberLabel: (member: EgressMember) => string,
  inheriting: boolean,
): { text: string; tone: 'normal' | 'muted' | 'hidden' } {
  if (!resolved) return { text: '未配置', tone: 'hidden' }
  if (resolved.egress.hidden) return { text: '不可见', tone: 'hidden' }
  const names = resolved.egress.members.map(memberLabel)
  const text = names.length === 0 ? '（空）' : names.join(' · ')
  return { text: `${resolved.egress.type} · ${text}`, tone: inheriting ? 'muted' : 'normal' }
}

function CellButton({
  summary,
  onClick,
}: {
  summary: { text: string; tone: 'normal' | 'muted' | 'hidden' }
  onClick: () => void
}) {
  return (
    <TableCell>
      <button
        type="button"
        className="w-full rounded-md px-2 py-1 text-left text-sm hover:bg-accent"
        onClick={onClick}
      >
        <span
          className={
            summary.tone === 'muted'
              ? 'text-muted-foreground'
              : summary.tone === 'hidden'
                ? 'text-muted-foreground italic'
                : undefined
          }
        >
          {summary.text}
        </span>
      </button>
    </TableCell>
  )
}

function PolicyDialog({
  row,
  options,
  onClose,
  onChange,
  onSetFinal,
}: {
  row: Row | null
  options: RoutingMatrix['options']
  onClose: () => void
  onChange: (policy: Policy) => void
  onSetFinal: () => void
}) {
  const policy = row?.policy
  return (
    <Dialog open={policy !== undefined} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>编辑分流策略</DialogTitle>
          <DialogDescription>
            这份规则清单对所有方案都一样；每个方案走哪条线路，由矩阵里的出口决定。
          </DialogDescription>
        </DialogHeader>
        {policy && (
          <FieldGroup>
            <FieldGroup className="grid gap-4 sm:grid-cols-[6rem_1fr]">
              <Field>
                <FieldLabel htmlFor="policy-icon">图标</FieldLabel>
                <Input
                  id="policy-icon"
                  placeholder="🤖"
                  value={policy.icon}
                  onChange={(event) => onChange({ ...policy, icon: event.target.value })}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="policy-name">名称</FieldLabel>
                <Input
                  id="policy-name"
                  value={policy.name}
                  onChange={(event) => onChange({ ...policy, name: event.target.value })}
                />
                <FieldDescription>客户端里的策略组名称就是「图标 + 名称」。</FieldDescription>
              </Field>
            </FieldGroup>

            <FieldGroup className="grid gap-4 sm:grid-cols-2">
              <Field orientation="horizontal">
                <FieldLabel htmlFor="policy-enabled">启用</FieldLabel>
                <Switch
                  id="policy-enabled"
                  checked={policy.enabled}
                  onCheckedChange={(enabled) => onChange({ ...policy, enabled })}
                />
              </Field>
              <Field orientation="horizontal">
                <FieldLabel htmlFor="policy-final">作为兜底（MATCH）</FieldLabel>
                <Switch id="policy-final" checked={policy.isFinal} onCheckedChange={() => onSetFinal()} />
              </Field>
            </FieldGroup>

            <FieldSet>
              <FieldLegend variant="label">规则清单（{policy.rules.length} 条）</FieldLegend>
              <FieldDescription>自上而下匹配。清单为空也没关系——那就是个纯粹给用户手切的选择器。</FieldDescription>
              <ScrollArea className="max-h-72 rounded-lg border">
                <div className="flex flex-col gap-2 p-3">
                  {policy.rules.map((rule, index) => (
                    <div key={index} className="flex flex-wrap items-center gap-2">
                      <Select
                        items={options.ruleTypes.map((value) => ({ value, label: value }))}
                        value={rule.type}
                        onValueChange={(value) =>
                          onChange({
                            ...policy,
                            rules: policy.rules.map((r, i) => (i === index ? { ...r, type: String(value) } : r)),
                          })
                        }
                      >
                        <SelectTrigger className="w-44" aria-label={`规则 ${index + 1} 的匹配类型`}>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectGroup>
                            {options.ruleTypes.map((value) => (
                              <SelectItem key={value} value={value}>
                                {value}
                              </SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <Input
                        className="flex-1"
                        aria-label={`规则 ${index + 1} 的内容`}
                        placeholder="例如 openai"
                        aria-invalid={!rule.value.trim() || undefined}
                        value={rule.value}
                        onChange={(event) =>
                          onChange({
                            ...policy,
                            rules: policy.rules.map((r, i) =>
                              i === index ? { ...r, value: event.target.value } : r,
                            ),
                          })
                        }
                      />
                      <label className="flex items-center gap-1 text-sm text-muted-foreground">
                        <Checkbox
                          aria-label={`规则 ${index + 1} 不解析域名`}
                          disabled={!options.noResolveTypes[rule.type]}
                          checked={rule.noResolve && Boolean(options.noResolveTypes[rule.type])}
                          onCheckedChange={(checked) =>
                            onChange({
                              ...policy,
                              rules: policy.rules.map((r, i) =>
                                i === index ? { ...r, noResolve: Boolean(checked) } : r,
                              ),
                            })
                          }
                        />
                        no-resolve
                      </label>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={`上移规则 ${index + 1}`}
                        disabled={index === 0}
                        onClick={() => onChange({ ...policy, rules: move(policy.rules, index, -1) })}
                      >
                        ↑
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={`下移规则 ${index + 1}`}
                        disabled={index === policy.rules.length - 1}
                        onClick={() => onChange({ ...policy, rules: move(policy.rules, index, 1) })}
                      >
                        ↓
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={`删除规则 ${index + 1}`}
                        onClick={() =>
                          onChange({ ...policy, rules: policy.rules.filter((_, i) => i !== index) })
                        }
                      >
                        <TrashIcon />
                      </Button>
                    </div>
                  ))}
                  <div>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() =>
                        onChange({
                          ...policy,
                          rules: [
                            ...policy.rules,
                            { type: 'GEOSITE', value: '', noResolve: false, disabled: false },
                          ],
                        })
                      }
                    >
                      <PlusIcon data-icon="inline-start" />
                      添加规则
                    </Button>
                  </div>
                </div>
              </ScrollArea>
            </FieldSet>

            <Field>
              <FieldLabel htmlFor="policy-remark">备注</FieldLabel>
              <Input
                id="policy-remark"
                value={policy.remark}
                onChange={(event) => onChange({ ...policy, remark: event.target.value })}
              />
            </Field>
          </FieldGroup>
        )}
        <DialogFooter>
          <Button onClick={onClose}>完成</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function EgressDialog({
  open,
  policyName,
  profileName,
  isDefaultColumn,
  own,
  inherited,
  options,
  groups,
  policies,
  memberLabel,
  onClose,
  onChange,
}: {
  open: boolean
  policyName: string
  profileName: string
  isDefaultColumn: boolean
  own: Egress | null
  inherited: Egress | null
  options: RoutingMatrix['options']
  groups: NodeGroup[]
  policies: Policy[]
  memberLabel: (member: EgressMember) => string
  onClose: () => void
  onChange: (egress: Egress | null) => void
}) {
  const mode: 'inherit' | 'custom' | 'hidden' = own === null ? 'inherit' : own.hidden ? 'hidden' : 'custom'
  const current = own ?? inherited

  const setMode = (next: 'inherit' | 'custom' | 'hidden') => {
    if (next === 'inherit') return onChange(null)
    if (next === 'hidden') return onChange({ ...(current ?? blankEgress(0)), hidden: true })
    onChange({ ...(current ?? blankEgress(0)), hidden: false })
  }

  const patch = (values: Partial<Egress>) => {
    if (!current) return
    onChange({ ...current, ...values, hidden: false })
  }

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>
            「{policyName}」在{profileName}下的出口
          </DialogTitle>
          <DialogDescription>
            {isDefaultColumn
              ? '默认出口是每个方案的兜底；写「全部节点组」就能让每个方案自动收窄到它自己的白名单。'
              : '只在和默认不同的时候才需要覆盖。'}
          </DialogDescription>
        </DialogHeader>

        <FieldGroup>
          {!isDefaultColumn && (
            <Field>
              <FieldLabel htmlFor="egress-mode">这一格</FieldLabel>
              <Select
                items={[
                  { value: 'inherit', label: '跟随默认' },
                  { value: 'custom', label: '自定义出口' },
                  { value: 'hidden', label: '不可见（这个方案没有这条分流）' },
                ]}
                value={mode}
                onValueChange={(value) => setMode(value as 'inherit' | 'custom' | 'hidden')}
              >
                <SelectTrigger id="egress-mode" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="inherit">跟随默认</SelectItem>
                    <SelectItem value="custom">自定义出口</SelectItem>
                    <SelectItem value="hidden">不可见（这个方案没有这条分流）</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
              {mode === 'hidden' && (
                <FieldDescription>它的策略组和规则都不会下发，这类流量会落到后面的规则或兜底。</FieldDescription>
              )}
            </Field>
          )}

          {mode !== 'hidden' && current && (
            <>
              <FieldGroup className="grid gap-4 sm:grid-cols-2">
                <Field>
                  <FieldLabel htmlFor="egress-type">选择方式</FieldLabel>
                  <Select
                    items={options.groupTypes.map((value) => ({ value, label: value }))}
                    value={current.type}
                    onValueChange={(value) => patch({ type: String(value) })}
                  >
                    <SelectTrigger id="egress-type" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {options.groupTypes.map((value) => (
                          <SelectItem key={value} value={value}>
                            {value}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
                {current.type !== 'select' && (
                  <Field>
                    <FieldLabel htmlFor="egress-interval">测速间隔 (秒)</FieldLabel>
                    <Input
                      id="egress-interval"
                      type="number"
                      min={0}
                      value={current.interval}
                      onChange={(event) => patch({ interval: Number(event.target.value) })}
                    />
                  </Field>
                )}
              </FieldGroup>

              <FieldSet>
                <FieldLegend variant="label">成员</FieldLegend>
                <FieldDescription>顺序就是客户端里的排列顺序，解析不到的成员会被自动跳过。</FieldDescription>
                <div className="flex flex-wrap items-center gap-2">
                  {current.members.map((member, index) => (
                    <Badge key={index} variant="secondary" className="gap-1 pr-1">
                      {memberLabel(member)}
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        className="size-5"
                        aria-label={`把成员 ${index + 1} 前移`}
                        disabled={index === 0}
                        onClick={() => patch({ members: move(current.members, index, -1) })}
                      >
                        ←
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        className="size-5"
                        aria-label={`移除成员 ${index + 1}`}
                        onClick={() => patch({ members: current.members.filter((_, i) => i !== index) })}
                      >
                        ×
                      </Button>
                    </Badge>
                  ))}
                  <MemberPicker
                    groups={groups}
                    policies={policies.filter((p) => p.name !== policyName)}
                    builtins={options.builtins}
                    onPick={(member) => patch({ members: [...current.members, member] })}
                  />
                </div>
                {current.members.length === 0 && (
                  <FieldDescription>成员为空的策略组会被渲染时丢弃，指向它的规则会落到兜底。</FieldDescription>
                )}
              </FieldSet>
            </>
          )}

          {mode === 'inherit' && inherited && (
            <FieldDescription>
              当前跟随默认：{inherited.type} · {inherited.members.map(memberLabel).join(' · ') || '（空）'}
            </FieldDescription>
          )}
        </FieldGroup>

        <DialogFooter>
          <Button onClick={onClose}>完成</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function MemberPicker({
  groups,
  policies,
  builtins,
  onPick,
}: {
  groups: NodeGroup[]
  policies: Policy[]
  builtins: string[]
  onPick: (member: EgressMember) => void
}) {
  const pick = (kind: MemberKind, ref: string) => onPick({ kind, ref })
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="outline" size="sm" />}>
        <PlusIcon data-icon="inline-start" />
        添加成员
        <ChevronDownIcon data-icon="inline-end" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="max-h-80 min-w-56 overflow-y-auto">
        <DropdownMenuGroup>
          <DropdownMenuLabel>动态展开</DropdownMenuLabel>
          <DropdownMenuItem onClick={() => pick('all-node-groups', '')}>
            全部节点组（按方案收窄）
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => pick('all-inbounds', '')}>全部入站</DropdownMenuItem>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        {groups.length > 0 && (
          <>
            <DropdownMenuGroup>
              <DropdownMenuLabel>节点组</DropdownMenuLabel>
              {groups.map((group) => (
                <DropdownMenuItem key={group.id} onClick={() => pick('node-group', group.name)}>
                  {group.emoji ? `${group.emoji} ${group.name}` : group.name}
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
          </>
        )}
        {policies.length > 0 && (
          <>
            <DropdownMenuGroup>
              <DropdownMenuLabel>分流策略</DropdownMenuLabel>
              {policies.map((policy) => (
                <DropdownMenuItem key={policy.name} onClick={() => pick('policy', policy.name)}>
                  {policy.icon ? `${policy.icon} ${policy.name}` : policy.name}
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
          </>
        )}
        <DropdownMenuGroup>
          <DropdownMenuLabel>内置策略</DropdownMenuLabel>
          {builtins.map((builtin) => (
            <DropdownMenuItem key={builtin} onClick={() => pick('builtin', builtin)}>
              {builtin}
            </DropdownMenuItem>
          ))}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function ProfileDialog({
  draft,
  groups,
  onChange,
  onClose,
  onSave,
}: {
  draft: ProfileDraft | null
  groups: NodeGroup[]
  onChange: (draft: ProfileDraft) => void
  onClose: () => void
  onSave: () => void
}) {
  return (
    <Dialog open={draft !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{draft?.id ? '编辑分流方案' : '新建分流方案'}</DialogTitle>
          <DialogDescription>
            可用节点组决定这个方案的用户能连上哪些入站。保存后会立刻同步到相关面板。
          </DialogDescription>
        </DialogHeader>
        {draft && (
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="profile-name">名称</FieldLabel>
              <Input
                id="profile-name"
                placeholder="VIP"
                value={draft.name}
                onChange={(event) => onChange({ ...draft, name: event.target.value })}
              />
            </Field>

            <Field orientation="horizontal">
              <FieldLabel htmlFor="profile-all">包含全部节点组</FieldLabel>
              <Switch
                id="profile-all"
                checked={draft.allGroups}
                onCheckedChange={(allGroups) => onChange({ ...draft, allGroups })}
              />
            </Field>

            {!draft.allGroups && (
              <FieldSet>
                <FieldLegend variant="label">可用节点组（已选 {draft.groupIds.length} 个）</FieldLegend>
                <ScrollArea className="h-56 rounded-lg border">
                  <FieldGroup className="gap-2 p-3">
                    {groups.length === 0 ? (
                      <FieldDescription>还没有节点组，先去「节点组」页建一个。</FieldDescription>
                    ) : (
                      groups.map((group) => (
                        <Field key={group.id} orientation="horizontal">
                          <Checkbox
                            id={`profile-group-${group.id}`}
                            checked={draft.groupIds.includes(group.id)}
                            onCheckedChange={() =>
                              onChange({
                                ...draft,
                                groupIds: draft.groupIds.includes(group.id)
                                  ? draft.groupIds.filter((id) => id !== group.id)
                                  : [...draft.groupIds, group.id],
                              })
                            }
                          />
                          <FieldLabel htmlFor={`profile-group-${group.id}`}>
                            <span>{group.emoji ? `${group.emoji} ${group.name}` : group.name}</span>
                            <span className="text-muted-foreground">{group.usableInbounds} 个入站</span>
                            {!group.enabled && <StatusBadge tone="idle">停用</StatusBadge>}
                          </FieldLabel>
                        </Field>
                      ))
                    )}
                  </FieldGroup>
                </ScrollArea>
              </FieldSet>
            )}

            <Field orientation="horizontal">
              <FieldLabel htmlFor="profile-enabled">启用</FieldLabel>
              <Switch
                id="profile-enabled"
                checked={draft.enabled}
                onCheckedChange={(enabled) => onChange({ ...draft, enabled })}
              />
            </Field>

            <Field>
              <FieldLabel htmlFor="profile-remark">备注</FieldLabel>
              <Input
                id="profile-remark"
                value={draft.remark}
                onChange={(event) => onChange({ ...draft, remark: event.target.value })}
              />
            </Field>
          </FieldGroup>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button disabled={!draft?.name.trim()} onClick={onSave}>
            保存方案
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

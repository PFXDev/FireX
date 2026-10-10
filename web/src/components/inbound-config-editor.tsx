import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'

import { PlusIcon, Trash2Icon } from 'lucide-react'

import { CodeText, CodeTextarea } from '@/components/code-display'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { parameterLabel, parameterLabels } from '@/lib/inbound-labels'
import { concealed, isObject, secretParameter } from '@/lib/inbound-parameters'

export type Parameters = Record<string, unknown>

export type ConfigChange = { path: string; before: unknown; after: unknown }


const sections = [
  { key: 'connection', label: '连接' },
  { key: 'settings', label: '协议' },
  { key: 'streamSettings', label: '传输与安全' },
  { key: 'sniffing', label: '嗅探' },
] as const

export function display(value: unknown, path: string, reveal: boolean) {
  if (value === undefined) return '未设置'
  const shown = reveal ? value : concealed(value, path.split('.').at(-1))
  return typeof shown === 'string' ? shown || '空字符串' : JSON.stringify(shown)
}

function blankValue(value: unknown): unknown {
  if (isObject(value)) return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, blankValue(item)]))
  if (Array.isArray(value)) return []
  if (typeof value === 'number') return 0
  if (typeof value === 'boolean') return false
  if (value === null) return null
  return ''
}

const Validation = createContext<(path: string, invalid: boolean) => void>(() => undefined)

// Raw JSON uses a local buffer so incomplete edits cannot erase saved values.
function JSONText({ id, value, disabled, onChange }: { id: string; value: unknown; disabled: boolean; onChange: (value: unknown) => void }) {
  const serialized = JSON.stringify(value, null, 2)
  const [text, setText] = useState(serialized)
  const [error, setError] = useState(false)
  const validation = useContext(Validation)
  useEffect(() => {
    validation(id, error)
    return () => validation(id, false)
  }, [id, error, validation])
  useEffect(() => {
    setText(serialized)
    setError(false)
  }, [serialized])
  return (
    <>
      <CodeTextarea
        id={id}
        value={text}
        disabled={disabled}
        aria-invalid={error || undefined}
        className="min-h-24"
        onChange={(event) => {
          setText(event.target.value)
          try {
            const parsed: unknown = JSON.parse(event.target.value)
            if (Array.isArray(value) && !Array.isArray(parsed)) throw new Error()
            setError(false)
            onChange(parsed)
          } catch {
            setError(true)
          }
        }}
      />
      {error && <FieldError>JSON 格式无效，尚未应用这段编辑。</FieldError>}
    </>
  )
}

// Recursive controls retain extension fields and JSON types.
function JSONValue({ value, onChange, path, disabled, reveal }: {
  value: unknown
  onChange: (value: unknown) => void
  path: string
  disabled: boolean
  reveal: boolean
}) {
  const key = path.split('.').at(-1) || path
  const id = `parameter-${path}`
  if (isObject(value))
    return (
      <FieldSet className="min-w-0">
        <FieldLegend variant="label">{parameterLabel(key)}</FieldLegend>
        <FieldGroup className="pl-3">
          {Object.entries(value).map(([name, item]) => (
            <JSONValue key={name} path={`${path}.${name}`} value={item} disabled={disabled} reveal={reveal} onChange={(next) => onChange({ ...value, [name]: next })} />
          ))}
        </FieldGroup>
      </FieldSet>
    )
  if (Array.isArray(value))
    return (
      <FieldSet className="min-w-0">
        <FieldLegend variant="label">{parameterLabel(key)}</FieldLegend>
        <FieldGroup className="pl-3">
          {value.map((item, index) => (
            <div key={index} className="flex items-start gap-2">
              <div className="min-w-0 flex-1">
                <JSONValue path={`${path}.${index}`} value={item} disabled={disabled} reveal={reveal} onChange={(next) => onChange(value.map((old, i) => (i === index ? next : old)))} />
              </div>
              <Button type="button" variant="ghost" size="sm" disabled={disabled} aria-label={`移除 ${path} 第 ${index + 1} 项`} onClick={() => onChange(value.filter((_, i) => i !== index))}>
                <Trash2Icon data-icon="inline-start" />
                <span className="hidden sm:inline">移除</span>
              </Button>
            </div>
          ))}
          <Button type="button" variant="outline" size="sm" className="self-start" disabled={disabled} onClick={() => onChange([...value, blankValue(value[0])])}>
            <PlusIcon data-icon="inline-start" />
            添加条目
          </Button>
        </FieldGroup>
      </FieldSet>
    )
  const secret = secretParameter(value, path)
  return (
    <Field data-disabled={disabled || undefined}>
      <FieldLabel htmlFor={id}>{parameterLabels[key] || (/^\d+$/.test(key) ? `第 ${Number(key) + 1} 项` : key)}</FieldLabel>
      {typeof value === 'boolean' ? (
        <Switch id={id} checked={value} disabled={disabled} onCheckedChange={onChange} />
      ) : value === null ? (
        <JSONText id={id} value={value} disabled={disabled || (secret && !reveal)} onChange={onChange} />
      ) : (
        <Input
          id={id}
          type={secret && !reveal ? 'password' : typeof value === 'number' ? 'number' : 'text'}
          disabled={disabled}
          value={String(value ?? '')}
          onChange={(event) => {
            const next = event.target.value
            if (typeof value !== 'number') onChange(next)
            else if (next !== '' && Number.isFinite(Number(next))) onChange(Number(next))
          }}
        />
      )}
      <FieldDescription><CodeText>{path}</CodeText></FieldDescription>
    </Field>
  )
}

/**
 * Edits a complete inbound configuration by section or as raw JSON. Secrets
 * stay masked, and raw JSON is read-only, until reveal is on.
 */
export function ConfigEditor({ parameters, onChange, onInvalidChange, disabled, reveal, children }: {
  parameters: Parameters
  onChange: (next: Parameters) => void
  onInvalidChange: (invalid: boolean) => void
  disabled: boolean
  reveal: boolean
  /** Extra fields shown first on the connection tab. */
  children?: React.ReactNode
}) {
  const [tab, setTab] = useState('connection')
  const [invalid, setInvalid] = useState<Set<string>>(new Set())
  const [raw, setRaw] = useState(() => JSON.stringify(parameters, null, 2))
  const [rawError, setRawError] = useState('')
  const editingRaw = useRef(false)

  const validate = useCallback((path: string, bad: boolean) => {
    setInvalid((previous) => {
      if (previous.has(path) === bad) return previous
      const next = new Set(previous)
      if (bad) next.add(path)
      else next.delete(path)
      return next
    })
  }, [])

  useEffect(() => {
    if (editingRaw.current) editingRaw.current = false
    else {
      setRaw(JSON.stringify(parameters, null, 2))
      setRawError('')
    }
  }, [parameters])

  const hasError = invalid.size > 0 || !!rawError
  useEffect(() => onInvalidChange(hasError), [hasError, onInvalidChange])

  const base = Object.fromEntries(Object.entries(parameters).filter(([key]) => !sections.some((section) => section.key === key)))
  return (
    <Validation.Provider value={validate}>
      <Tabs
        value={tab}
        onValueChange={(value) => {
          if (hasError) return
          setTab(String(value))
        }}
      >
        <TabsList className="flex h-auto flex-wrap">
          {sections.map((section) => (
            <TabsTrigger key={section.key} value={section.key}>{section.label}</TabsTrigger>
          ))}
          <TabsTrigger value="json">完整 JSON</TabsTrigger>
        </TabsList>
        {sections.map((section) => {
          const value = section.key === 'connection' ? base : parameters[section.key]
          return (
            <TabsContent key={section.key} value={section.key} className="pt-5">
              <FieldGroup>
                {section.key === 'connection' && children}
                {isObject(value) && Object.entries(value).map(([key, item]) => (
                  <JSONValue
                    key={key}
                    path={section.key === 'connection' ? key : `${section.key}.${key}`}
                    value={item}
                    disabled={disabled}
                    reveal={reveal}
                    onChange={(next) => onChange(section.key === 'connection'
                      ? { ...parameters, [key]: next }
                      : { ...parameters, [section.key]: { ...value, [key]: next } })}
                  />
                ))}
              </FieldGroup>
            </TabsContent>
          )
        })}
        <TabsContent value="json" className="pt-5">
          <Field data-invalid={!!rawError || undefined} data-disabled={disabled || !reveal || undefined}>
            <FieldLabel htmlFor="config-json">完整参数</FieldLabel>
            <CodeTextarea
              id="config-json"
              className="min-h-96"
              value={reveal ? raw : JSON.stringify(concealed(parameters), null, 2)}
              disabled={disabled || !reveal}
              aria-invalid={!!rawError || undefined}
              onChange={(event) => {
                setRaw(event.target.value)
                try {
                  const value: unknown = JSON.parse(event.target.value)
                  if (!isObject(value)) throw new Error()
                  editingRaw.current = true
                  setRawError('')
                  onChange(value)
                } catch {
                  setRawError('必须是有效的 JSON 对象')
                }
              }}
            />
            <FieldDescription>打开「显示密钥」后可编辑、添加或移除任意字段。客户端、流量计数与面板编号不在这里保存。</FieldDescription>
            {rawError && <FieldError>{rawError}</FieldError>}
          </Field>
        </TabsContent>
      </Tabs>
    </Validation.Provider>
  )
}

export function ChangesTable({ changes, reveal }: { changes: ConfigChange[]; reveal: boolean }) {
  if (changes.length === 0)
    return (
      <Empty className="border">
        <EmptyHeader>
          <EmptyTitle>没有差异</EmptyTitle>
          <EmptyDescription>面板参数与配置一致。</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>字段</TableHead>
          <TableHead>面板当前</TableHead>
          <TableHead>下发后</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {changes.map((change) => (
          <TableRow key={change.path}>
            <TableCell className="max-w-48 break-all whitespace-normal"><CodeText className="text-foreground">{change.path}</CodeText></TableCell>
            <TableCell className="max-w-48 break-all whitespace-normal"><CodeText>{display(change.before, change.path, reveal)}</CodeText></TableCell>
            <TableCell className="max-w-48 break-all whitespace-normal"><CodeText className="text-foreground">{display(change.after, change.path, reveal)}</CodeText></TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

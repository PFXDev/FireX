import { useState } from 'react'
import { CopyIcon, DicesIcon, EyeIcon, EyeOffIcon } from 'lucide-react'
import { toast } from 'sonner'

import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from '@/components/ui/input-group'
import { copyText } from '@/lib/format'

const PASSWORD_ALPHABET = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%&*-_'

function randomPassword(length = 16): string {
  const bytes = new Uint32Array(length)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (b) => PASSWORD_ALPHABET[b % PASSWORD_ALPHABET.length]).join('')
}

/**
 * PasswordInput wraps InputGroup with a visibility toggle and optional random
 * generation / clipboard copy controls for admin credentials.
 */
export function PasswordInput({
  value,
  onChange,
  onChangeValue,
  allowGenerate = false,
  allowCopy = false,
  generateLength = 16,
  disabled,
  className,
  ...props
}: Omit<React.ComponentProps<typeof InputGroupInput>, 'type' | 'value'> & {
  value: string
  onChangeValue?: (value: string) => void
  allowGenerate?: boolean
  allowCopy?: boolean
  generateLength?: number
}) {
  const [visible, setVisible] = useState(false)

  const emitValue = (next: string) => {
    onChangeValue?.(next)
  }

  return (
    <InputGroup className={className} data-disabled={disabled || undefined}>
      <InputGroupInput
        {...props}
        type={visible ? 'text' : 'password'}
        value={value}
        disabled={disabled}
        onChange={(event) => {
          onChange?.(event)
          emitValue(event.target.value)
        }}
      />
      <InputGroupAddon align="inline-end">
        {allowGenerate && (
          <InputGroupButton
            size="icon-xs"
            disabled={disabled}
            aria-label="生成随机密码"
            title="生成随机密码"
            onClick={() => {
              const generated = randomPassword(generateLength)
              setVisible(true)
              emitValue(generated)
            }}
          >
            <DicesIcon />
          </InputGroupButton>
        )}
        {allowCopy && value && (
          <InputGroupButton
            size="icon-xs"
            disabled={disabled}
            aria-label="复制密码"
            title="复制密码"
            onClick={() => {
              void copyText(value).then((ok) => {
                if (ok) toast.success('密码已复制')
                else toast.error('复制失败，请手动复制')
              })
            }}
          >
            <CopyIcon />
          </InputGroupButton>
        )}
        <InputGroupButton
          size="icon-xs"
          disabled={disabled}
          aria-label={visible ? '隐藏密码' : '显示密码'}
          title={visible ? '隐藏密码' : '显示密码'}
          onClick={() => setVisible((current) => !current)}
        >
          {visible ? <EyeOffIcon /> : <EyeIcon />}
        </InputGroupButton>
      </InputGroupAddon>
    </InputGroup>
  )
}

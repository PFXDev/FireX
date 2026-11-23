import { GitCommitHorizontalIcon } from 'lucide-react'
import Markdown from 'react-markdown'

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

export function ReleaseNotes({ notes }: { notes: string }) {
  return (
    <Card size="sm" className="bg-muted/30">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <GitCommitHorizontalIcon className="size-4 text-muted-foreground" />
          变更说明
        </CardTitle>
        <CardDescription>此版本的提交变更摘要。</CardDescription>
      </CardHeader>
      <CardContent>
        <div
          role="region"
          aria-label="变更说明"
          tabIndex={0}
          className="max-h-72 overflow-y-auto overscroll-contain rounded-sm pr-2 outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <div className="flex flex-col gap-3 text-sm leading-relaxed wrap-anywhere [&_h1]:font-semibold [&_h2]:font-semibold [&_h3]:pt-2 [&_h3]:font-medium [&_h4]:font-medium [&_p]:text-muted-foreground [&_ul]:flex [&_ul]:list-disc [&_ul]:flex-col [&_ul]:gap-2 [&_ul]:pl-4 [&_ol]:list-decimal [&_ol]:pl-4 [&_li]:pl-1 [&_li::marker]:text-muted-foreground [&_code]:rounded [&_code]:bg-muted [&_code]:px-1.5 [&_code]:py-0.5 [&_code]:font-mono [&_code]:text-xs [&_pre]:overflow-x-auto [&_pre]:rounded-md [&_pre]:bg-muted [&_pre]:p-3 [&_pre_code]:bg-transparent [&_pre_code]:p-0 [&_blockquote]:border-l-2 [&_blockquote]:pl-3">
            <Markdown
              skipHtml
              disallowedElements={['img']}
              components={{
                a: ({ href, children }) =>
                  href ? (
                    <a
                      href={href}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="rounded-sm text-primary underline decoration-primary/30 underline-offset-4 hover:decoration-primary focus-visible:outline-2 focus-visible:outline-ring"
                    >
                      {children}
                    </a>
                  ) : (
                    <span>{children}</span>
                  ),
              }}
            >
              {notes}
            </Markdown>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

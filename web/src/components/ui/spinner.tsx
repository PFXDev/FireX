import { cn } from "@/lib/utils"
import { Loader2Icon } from "lucide-react"

function Spinner({ className, "aria-label": ariaLabel = "加载中", ...props }: React.ComponentProps<"svg">) {
  return (
    <Loader2Icon data-slot="spinner" role="status" aria-label={ariaLabel} className={cn("size-4 animate-spin", className)} {...props} />
  )
}

export { Spinner }

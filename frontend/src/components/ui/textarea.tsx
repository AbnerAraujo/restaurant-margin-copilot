import * as React from "react"

import { cn } from "@/lib/utils"

function Textarea({ className, ...props }: React.ComponentProps<"textarea">) {
  return (
    <textarea
      data-slot="textarea"
      className={cn(
        // `field-sizing-content` grows the box with its value and has no
        // upper bound of its own, so a long paste grows the field until it
        // pushes whatever sits below it (a Send button, a form's actions)
        // off screen. `max-h-64` plus `overflow-y-auto` gives the growth a
        // ceiling and lets the field scroll internally past it.
        "flex field-sizing-content max-h-64 min-h-16 w-full overflow-y-auto rounded-md border border-input bg-transparent px-3 py-2 text-base shadow-xs transition-[color,box-shadow] outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-destructive/20 md:text-sm dark:bg-input/30 dark:aria-invalid:ring-destructive/40",
        className
      )}
      {...props}
    />
  )
}

export { Textarea }

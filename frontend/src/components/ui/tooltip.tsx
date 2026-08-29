"use client"

import * as React from "react"
import { Tooltip as TooltipPrimitive } from "radix-ui"

import { cn } from "@/lib/utils"

function TooltipProvider({
  // 300ms: long enough that a cursor merely passing over a trigger (moving
  // toward something else) doesn't flash a tooltip open, short enough that a
  // deliberate hover still feels immediate. The previous 200ms sat right in
  // the "did that just flicker?" zone on a quick mouse pass across a toolbar.
  delayDuration = 300,
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Provider>) {
  return (
    <TooltipPrimitive.Provider
      data-slot="tooltip-provider"
      delayDuration={delayDuration}
      {...props}
    />
  )
}

function Tooltip({
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Root>) {
  return (
    <TooltipProvider>
      <TooltipPrimitive.Root data-slot="tooltip" {...props} />
    </TooltipProvider>
  )
}

function TooltipTrigger({
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Trigger>) {
  return <TooltipPrimitive.Trigger data-slot="tooltip-trigger" {...props} />
}

function TooltipContent({
  className,
  sideOffset = 6,
  // 8px of breathing room against the viewport edge before Radix's own
  // collision detection repositions the tooltip — without this a trigger
  // near the window edge could still touch it flush.
  collisionPadding = 8,
  children,
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Content>) {
  return (
    <TooltipPrimitive.Portal>
      <TooltipPrimitive.Content
        data-slot="tooltip-content"
        sideOffset={sideOffset}
        collisionPadding={collisionPadding}
        className={cn(
          // Widened from max-w-64 (256px): several tooltips in this app carry
          // a full explanatory sentence or two (e.g. Stat's info tooltips),
          // and at the old width those wrapped into a tall, narrow column of
          // 8-10 short lines. This width, plus leading-relaxed and
          // text-pretty (avoids a lonely last word/orphan), keeps a
          // paragraph-length tooltip to a readable 3-4 lines while a short
          // one-word hint still hugs its content instead of stretching.
          // Capped against the viewport (calc(100vw-2rem)) so it can never
          // overflow a narrow window itself.
          "z-50 w-max max-w-[min(20rem,calc(100vw-2rem))] rounded-lg border border-border bg-popover px-3 py-2 text-xs leading-relaxed text-pretty text-popover-foreground shadow-lg",
          "animate-in fade-in-0 zoom-in-95 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95",
          "data-[side=bottom]:slide-in-from-top-1 data-[side=left]:slide-in-from-right-1 data-[side=right]:slide-in-from-left-1 data-[side=top]:slide-in-from-bottom-1",
          className
        )}
        {...props}
      >
        {children}
        <TooltipPrimitive.Arrow className="fill-popover" width={11} height={5} />
      </TooltipPrimitive.Content>
    </TooltipPrimitive.Portal>
  )
}

export { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider }

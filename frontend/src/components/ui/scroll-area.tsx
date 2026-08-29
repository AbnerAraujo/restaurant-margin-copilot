import * as React from "react"
import { ScrollArea as ScrollAreaPrimitive } from "radix-ui"

import { cn } from "@/lib/utils"

function ScrollArea({
  className,
  children,
  viewportRef,
  ...props
}: React.ComponentProps<typeof ScrollAreaPrimitive.Root> & {
  /**
   * Handle on the scrolling element itself. Radix nests the real scroll
   * container (`[data-radix-scroll-area-viewport]`) inside `Root`, so a ref
   * on `Root` points at a non-scrolling wrapper. A caller that has to drive
   * scroll position imperatively — a chat log pinning itself to the newest
   * message — needs the viewport, and reaching for it with a DOM query from
   * outside would couple that caller to Radix's internal markup.
   */
  viewportRef?: React.Ref<HTMLDivElement>
}) {
  return (
    <ScrollAreaPrimitive.Root
      data-slot="scroll-area"
      className={cn("relative", className)}
      {...props}
    >
      <ScrollAreaPrimitive.Viewport
        data-slot="scroll-area-viewport"
        ref={viewportRef}
        // overscroll-behavior: contain — without it, a wheel/trackpad
        // gesture that reaches the top or bottom of THIS viewport can chain
        // into the nearest scrollable ancestor (main's own overflow-y-auto)
        // instead of stopping, which on a trackpad's momentum scrolling
        // reads as "the chat's internal scroll doesn't work" even though
        // the viewport itself is scrolling correctly underneath — reported
        // live, not reproducible via synthetic wheel events (which don't
        // carry momentum), so this is the real, known fix for the class of
        // bug that symptom describes rather than a confirmed root cause.
        className="size-full overscroll-contain rounded-[inherit] transition-[color,box-shadow] outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-1"
      >
        {children}
      </ScrollAreaPrimitive.Viewport>
      <ScrollBar />
      <ScrollAreaPrimitive.Corner />
    </ScrollAreaPrimitive.Root>
  )
}

function ScrollBar({
  className,
  orientation = "vertical",
  ...props
}: React.ComponentProps<typeof ScrollAreaPrimitive.ScrollAreaScrollbar>) {
  return (
    <ScrollAreaPrimitive.ScrollAreaScrollbar
      data-slot="scroll-area-scrollbar"
      orientation={orientation}
      className={cn(
        "flex touch-none p-px transition-colors select-none",
        orientation === "vertical" &&
          "h-full w-2.5 border-l border-l-transparent",
        orientation === "horizontal" &&
          "h-2.5 flex-col border-t border-t-transparent",
        className
      )}
      {...props}
    >
      <ScrollAreaPrimitive.ScrollAreaThumb
        data-slot="scroll-area-thumb"
        className="relative flex-1 rounded-full bg-border"
      />
    </ScrollAreaPrimitive.ScrollAreaScrollbar>
  )
}

export { ScrollArea, ScrollBar }

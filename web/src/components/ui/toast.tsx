"use client"

import { Toast as ToastPrimitive } from "@base-ui/react/toast"
import { XIcon } from "lucide-react"
import type { ReactNode } from "react"

import { cn } from "@/lib/utils"

export type ToastTone = "success" | "destructive" | "warning"

/**
 * Base UI toast primitives, presented in the product's neutral scale.
 *
 * This module is the only place that knows the toast implementation; the
 * product API in `components/app/toast.tsx` never sees Base UI directly.
 */
function ToastProvider({
  children,
  limit = 3,
  timeout = 5000,
}: {
  children: ReactNode
  limit?: number
  timeout?: number
}) {
  return (
    <ToastPrimitive.Provider limit={limit} timeout={timeout}>
      {children}
    </ToastPrimitive.Provider>
  )
}

function useToastManager() {
  return ToastPrimitive.useToastManager()
}

const TONE_CLASS: Record<ToastTone, string> = {
  success: "border-success/45",
  warning: "border-warning/45",
  destructive: "border-destructive/45",
}

function ToastViewport({ closeLabel }: { closeLabel: string }) {
  const manager = useToastManager()

  return (
    <ToastPrimitive.Viewport className="fixed right-4 bottom-4 z-90 flex w-[min(380px,calc(100vw-32px))] flex-col gap-2">
      {manager.toasts.map((toast) => (
        <ToastPrimitive.Root
          key={toast.id}
          toast={toast}
          data-toast-tone={toast.type}
          className={cn(
            "border bg-card shadow-lg transition-opacity data-ending-style:opacity-0 data-starting-style:opacity-0",
            TONE_CLASS[toast.type as ToastTone] ?? "border-border",
          )}
        >
          <ToastPrimitive.Content className="flex min-h-12 items-center gap-2 py-1.5 pr-1.5 pl-3">
            <ToastPrimitive.Description className="flex-1 text-[13px] leading-[1.45] text-foreground" />
            <ToastPrimitive.Close
              aria-label={closeLabel}
              className="inline-flex size-10 shrink-0 items-center justify-center text-muted-foreground transition-colors hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
            >
              <XIcon className="size-5" />
            </ToastPrimitive.Close>
          </ToastPrimitive.Content>
        </ToastPrimitive.Root>
      ))}
    </ToastPrimitive.Viewport>
  )
}

export { ToastProvider, ToastViewport, useToastManager }

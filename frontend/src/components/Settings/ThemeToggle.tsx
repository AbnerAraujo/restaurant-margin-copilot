import { Monitor, Moon, Sun } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { useTheme, type ThemePreference } from '@/lib/theme'

const OPTIONS: { value: ThemePreference; label: string; icon: typeof Sun }[] = [
  { value: 'light', label: 'Light', icon: Sun },
  { value: 'dark', label: 'Dark', icon: Moon },
  { value: 'system', label: 'System', icon: Monitor },
]

/**
 * Light / Dark / System, styled as the same `role="radiogroup"` segmented
 * control `LogReplacementForm.tsx`'s "Pay with" picker already established
 * (three `Button`s as `role="radio"`, `aria-checked` driving `default`
 * vs. `ghost`) rather than a second, inconsistent control pattern. Every
 * option carries its own word — "Light"/"Dark"/"System" — never an
 * icon-only button, so the choice is legible without relying on the icon
 * alone to carry meaning.
 */
export function ThemeToggle() {
  const { preference, resolvedTheme, setPreference } = useTheme()

  return (
    <div className="flex flex-col gap-1.5">
      <div
        role="radiogroup"
        aria-label="Theme"
        className="inline-flex overflow-hidden rounded-md border border-border"
      >
        {OPTIONS.map(({ value, label, icon: Icon }) => (
          <Button
            key={value}
            type="button"
            role="radio"
            aria-checked={preference === value}
            variant={preference === value ? 'default' : 'ghost'}
            size="sm"
            className="rounded-none"
            onClick={() => setPreference(value)}
          >
            <Icon className="size-3.5" aria-hidden="true" />
            {label}
          </Button>
        ))}
      </div>
      {preference === 'system' ? (
        <p className="text-xs text-muted-foreground">
          Following your device, currently {resolvedTheme}.
        </p>
      ) : null}
    </div>
  )
}

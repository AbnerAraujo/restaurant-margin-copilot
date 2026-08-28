/**
 * Batwing café-door mark: tall frame posts, short hexagonal panels hinged
 * mid-frame, each tapering to a point at the center gap — the real anatomy
 * of a swinging kitchen/bar door. Green (not the app's brand-red predecessor)
 * for the "in the red / in the green" idiom this product's whole job is to
 * cross. Fixed swatch colors, not CSS custom properties — the mark keeps
 * its own identity independent of the current theme.
 */
export interface LogoProps {
  className?: string;
  /** Icon only, or icon + wordmark. Defaults to lockup. */
  variant?: "icon" | "lockup";
  size?: number;
}

const MARK_INK = "#100D0C";
const MARK_FRAME = "#5A6B5E";
const MARK_DOOR = "#1F9D55";

function StewardMark({ size = 40 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 100 100"
      role="img"
      aria-label="My Business Steward"
    >
      <rect x="6" y="6" width="88" height="88" rx="22" fill={MARK_INK} />
      <line x1="20" y1="14" x2="20" y2="90" stroke={MARK_FRAME} strokeWidth="4" />
      <line x1="80" y1="14" x2="80" y2="90" stroke={MARK_FRAME} strokeWidth="4" />
      <line x1="20" y1="14" x2="80" y2="14" stroke={MARK_FRAME} strokeWidth="4" />
      <polygon points="20,34 39,34 48,52 39,70 20,70" fill={MARK_DOOR} />
      <polygon points="80,34 61,34 52,52 61,70 80,70" fill={MARK_DOOR} />
    </svg>
  );
}

export default function Logo({ className, variant = "lockup", size = 36 }: LogoProps) {
  if (variant === "icon") {
    return <StewardMark size={size} />;
  }

  return (
    <div className={`flex items-center gap-2.5 ${className ?? ""}`}>
      <StewardMark size={size} />
      <div className="leading-tight">
        <div className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
          My Business
        </div>
        <div className="text-lg font-semibold tracking-tight text-foreground">
          Steward
        </div>
      </div>
    </div>
  );
}

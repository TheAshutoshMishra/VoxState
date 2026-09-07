type Variant = "success" | "danger" | "warning" | "info" | "neutral";

// Symbol + color together, never color alone (WCAG 1.4.1 / M8 §16) — a
// colorblind viewer, or a black-and-white printout of a demo screenshot,
// can still tell REJECTED from ACCEPTED from the glyph and the text label.
const VARIANT: Record<Variant, { symbol: string; classes: string }> = {
  success: { symbol: "✓", classes: "bg-emerald-950 text-emerald-300 border-emerald-800" },
  danger: { symbol: "✕", classes: "bg-red-950 text-red-300 border-red-800" },
  warning: { symbol: "▲", classes: "bg-amber-950 text-amber-300 border-amber-800" },
  info: { symbol: "●", classes: "bg-sky-950 text-sky-300 border-sky-800" },
  neutral: { symbol: "○", classes: "bg-zinc-900 text-zinc-400 border-zinc-700" },
};

export function StatusPill({
  label,
  variant,
}: {
  label: string;
  variant: Variant;
}) {
  const { symbol, classes } = VARIANT[variant];
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-semibold ${classes}`}
    >
      <span aria-hidden="true">{symbol}</span>
      {label}
    </span>
  );
}

export function taskStatusVariant(status: string): Variant {
  switch (status) {
    case "COMPLETED":
      return "success";
    case "CANCELLED":
      return "danger";
    case "RUNNING":
      return "info";
    default:
      return "warning";
  }
}

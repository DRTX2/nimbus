const relative = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
const absolute = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "medium" });

const units: [Intl.RelativeTimeFormatUnit, number][] = [
  ["day", 86_400],
  ["hour", 3_600],
  ["minute", 60],
  ["second", 1],
];

export function formatRelative(iso: string, now: Date = new Date()): string {
  const seconds = Math.round((new Date(iso).getTime() - now.getTime()) / 1000);
  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size || unit === "second") {
      return relative.format(Math.round(seconds / size), unit);
    }
  }
  return iso;
}

export function formatAbsolute(iso: string): string {
  return absolute.format(new Date(iso));
}

export function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3_600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
  return `${Math.floor(seconds / 3_600)}h ${Math.floor((seconds % 3_600) / 60)}m`;
}

export function formatPercent(ratio: number): string {
  return `${(ratio * 100).toFixed(ratio === 1 || ratio === 0 ? 0 : 1)}%`;
}

export function formatCount(value: number): string {
  return new Intl.NumberFormat().format(value);
}

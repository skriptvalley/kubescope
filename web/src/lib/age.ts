// Relative-age formatting for creationTimestamp columns, mirroring kubectl's
// compact style (45s, 12m, 5h, 3d, 2y). `now` is injectable for tests.

export function formatAge(iso: string | undefined, now: Date = new Date()): string {
  if (!iso) return "—";
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "—";

  let seconds = Math.floor((now.getTime() - then) / 1000);
  if (seconds < 0) seconds = 0;

  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return `${hours}h`;
  const days = Math.floor(hours / 24);
  if (days < 365) return `${days}d`;
  const years = Math.floor(days / 365);
  return `${years}y`;
}

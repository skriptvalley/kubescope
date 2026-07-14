import { cn } from "@/lib/utils";

/** All-namespaces is the empty-string value; any other value is a single namespace. */
export const ALL_NAMESPACES = "";

interface NamespaceSelectorProps {
  value: string;
  onChange: (value: string) => void;
  namespaces: string[];
  isLoading?: boolean;
  disabled?: boolean;
}

/** Drives namespaced list scope. Hidden by callers for cluster-scoped kinds. */
export function NamespaceSelector({
  value,
  onChange,
  namespaces,
  isLoading,
  disabled,
}: NamespaceSelectorProps) {
  // A controlled <select> with no matching <option> would silently display the
  // first option while the actual filter is `value` (e.g. a deep-linked or
  // since-deleted namespace, or before the list loads). Surface `value` as an
  // option so the shown namespace always matches the active filter.
  const options =
    value !== ALL_NAMESPACES && !namespaces.includes(value) ? [value, ...namespaces] : namespaces;

  return (
    <label className="flex items-center gap-2 text-sm">
      <span className="text-muted-foreground">Namespace</span>
      <select
        aria-label="Namespace"
        value={value}
        disabled={disabled || isLoading}
        onChange={(e) => onChange(e.target.value)}
        className={cn(
          "h-9 min-w-40 rounded-md border border-input bg-background px-2 text-sm",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
          "disabled:cursor-not-allowed disabled:opacity-50",
        )}
      >
        <option value={ALL_NAMESPACES}>{isLoading ? "Loading…" : "All namespaces"}</option>
        {options.map((ns) => (
          <option key={ns} value={ns}>
            {ns}
          </option>
        ))}
      </select>
    </label>
  );
}

import { Monitor, Moon, Sun } from "lucide-react";
import { useSyncExternalStore } from "react";

import { themeStore, type Theme } from "@/lib/theme";
import { cn } from "@/lib/utils";

const OPTIONS: { value: Theme; label: string; Icon: typeof Sun }[] = [
  { value: "light", label: "Light", Icon: Sun },
  { value: "system", label: "System", Icon: Monitor },
  { value: "dark", label: "Dark", Icon: Moon },
];

/** The header color-theme control: a segmented radiogroup (light / system /
 *  dark). Persists via themeStore and follows the OS in "system" mode. */
export function ThemeToggle() {
  const theme = useSyncExternalStore(themeStore.subscribe, themeStore.get, themeStore.get);

  return (
    <div
      role="radiogroup"
      aria-label="Color theme"
      className="inline-flex shrink-0 items-center gap-0.5 rounded-md border bg-card p-0.5"
    >
      {OPTIONS.map(({ value, label, Icon }) => {
        const active = theme === value;
        return (
          <button
            key={value}
            type="button"
            role="radio"
            aria-checked={active}
            aria-label={label}
            title={label}
            onClick={() => themeStore.set(value)}
            className={cn(
              "inline-flex h-7 w-7 items-center justify-center rounded-sm transition-colors",
              active
                ? "bg-primary text-primary-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <Icon className="h-[15px] w-[15px]" aria-hidden="true" />
          </button>
        );
      })}
    </div>
  );
}

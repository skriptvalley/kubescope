import { AlertTriangle, Search } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { Input } from "@/components/ui/input";
import { MIN_SEARCH_LENGTH, useSearch } from "@/hooks/use-search";
import { groupToken, type SearchResult } from "@/lib/api";
import { cn } from "@/lib/utils";

// Global search (Story 7.5): a name search across the active context's types.
// `/` focuses it from anywhere, arrow keys move the selection, Enter/click
// navigates to the matched object, Esc closes. Results degrade gracefully — a
// truncation notice and a per-type-failure warning render inline.

const DEBOUNCE_MS = 250;

/** Builds the detail route for a match (namespaced kinds carry the namespace). */
function resultHref(r: SearchResult): string {
  const base = `/resources/${groupToken(r.group)}/${r.version}/${r.resource}`;
  return r.namespaced && r.namespace
    ? `${base}/${encodeURIComponent(r.namespace)}/${encodeURIComponent(r.name)}`
    : `${base}/${encodeURIComponent(r.name)}`;
}

/** Whether focus is in a field where "/" should type a slash, not focus search. */
function inEditable(el: EventTarget | null): boolean {
  if (!(el instanceof HTMLElement)) return false;
  const tag = el.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el.isContentEditable;
}

export function GlobalSearch() {
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  // Debounce the query the search actually runs on.
  useEffect(() => {
    const id = setTimeout(() => setDebounced(query), DEBOUNCE_MS);
    return () => clearTimeout(id);
  }, [query]);

  const { data, isFetching } = useSearch(debounced, open);
  const results = data?.results ?? [];

  // Reset the highlighted row whenever the result set changes.
  useEffect(() => {
    setActive(0);
  }, [data]);

  // "/" focuses search from anywhere (unless typing in a field).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "/" && !inEditable(e.target)) {
        e.preventDefault();
        inputRef.current?.focus();
        setOpen(true);
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  // Close on outside click.
  useEffect(() => {
    if (!open) return;
    const onClick = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onClick);
    return () => document.removeEventListener("mousedown", onClick);
  }, [open]);

  const go = (r: SearchResult) => {
    navigate(resultHref(r));
    setOpen(false);
    setQuery("");
    inputRef.current?.blur();
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") {
      setOpen(false);
      inputRef.current?.blur();
      return;
    }
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setOpen(true);
      setActive((i) => Math.min(i + 1, results.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((i) => Math.max(i - 1, 0));
    } else if (e.key === "Enter" && results[active]) {
      e.preventDefault();
      go(results[active]);
    }
  };

  const showDropdown = open && debounced.trim().length >= MIN_SEARCH_LENGTH;

  return (
    <div ref={containerRef} className="relative w-full max-w-md">
      <div className="relative">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          ref={inputRef}
          type="text"
          role="combobox"
          aria-expanded={showDropdown}
          aria-controls="global-search-results"
          aria-label="Search resources"
          placeholder="Search resources…  (press /)"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setOpen(true);
          }}
          onFocus={() => setOpen(true)}
          onKeyDown={onKeyDown}
          className="pl-8"
        />
      </div>

      {showDropdown && (
        <div
          id="global-search-results"
          role="listbox"
          className="absolute z-20 mt-1 max-h-96 w-full overflow-y-auto rounded-md border bg-popover p-1 shadow-md"
        >
          {results.length === 0 ? (
            <p className="px-2 py-3 text-sm text-muted-foreground">
              {isFetching ? "Searching…" : `No matches for “${debounced.trim()}”.`}
            </p>
          ) : (
            <ul>
              {results.map((r, i) => (
                <li key={`${r.group}/${r.resource}/${r.namespace ?? ""}/${r.name}`}>
                  <button
                    type="button"
                    role="option"
                    aria-selected={i === active}
                    onMouseEnter={() => setActive(i)}
                    onClick={() => go(r)}
                    className={cn(
                      "flex w-full items-center justify-between gap-3 rounded-sm px-2 py-1.5 text-left text-sm",
                      i === active ? "bg-accent text-accent-foreground" : "hover:bg-accent/50",
                    )}
                  >
                    <span className="min-w-0 truncate font-medium">{r.name}</span>
                    <span className="shrink-0 text-xs text-muted-foreground">
                      {r.kind}
                      {r.namespace ? ` · ${r.namespace}` : ""}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}

          {data?.truncated && (
            <p className="px-2 py-1.5 text-xs text-muted-foreground">
              More matches exist — refine your search.
            </p>
          )}
          {data?.warnings && data.warnings.length > 0 && (
            <p
              className="flex items-center gap-1 px-2 py-1.5 text-xs text-muted-foreground"
              title={data.warnings.join("\n")}
            >
              <AlertTriangle className="h-3.5 w-3.5" />
              Partial results — some types could not be searched.
            </p>
          )}
        </div>
      )}
    </div>
  );
}

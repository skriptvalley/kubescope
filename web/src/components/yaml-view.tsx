import { tokenizeYamlLine, type YamlTokenKind } from "@/lib/yaml-highlight";
import { cn } from "@/lib/utils";

const tokenClass: Record<YamlTokenKind, string> = {
  key: "text-sky-700 dark:text-sky-300",
  value: "text-foreground",
  comment: "italic text-muted-foreground",
  plain: "text-muted-foreground",
};

/** Read-only, lightly syntax-highlighted YAML. Editing is Sprint 5. */
export function YamlView({ yaml }: { yaml: string }) {
  const lines = yaml.replace(/\n$/, "").split("\n");
  return (
    <pre
      data-testid="yaml-view"
      className="overflow-auto rounded-md border bg-muted/40 p-4 font-mono text-xs leading-relaxed"
    >
      <code>
        {lines.map((line, i) => (
          <div key={i}>
            {tokenizeYamlLine(line).map((token, j) => (
              <span key={j} className={cn(tokenClass[token.kind])}>
                {token.text}
              </span>
            ))}
            {line === "" ? " " : null}
          </div>
        ))}
      </code>
    </pre>
  );
}

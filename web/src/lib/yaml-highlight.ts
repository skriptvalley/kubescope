// Minimal, dependency-free YAML tokenizer for the read-only YAML tab. A real
// editor (CodeMirror) lands in Sprint 5; here we only need to distinguish keys,
// values and comments for lightweight highlighting. Pure and testable.

export type YamlTokenKind = "key" | "value" | "comment" | "plain";

export interface YamlToken {
  text: string;
  kind: YamlTokenKind;
}

// Matches an optional indent (and list dash), a mapping key, its colon, and the
// remainder. The key is non-greedy so `image: nginx:1.25` keys on the first colon.
const mappingLine = /^(\s*(?:-\s+)?)([^:\s][^:]*?)(:)(\s.*|$)/;

export function tokenizeYamlLine(line: string): YamlToken[] {
  if (line.trimStart().startsWith("#")) {
    return [{ text: line, kind: "comment" }];
  }
  const match = mappingLine.exec(line);
  if (!match) {
    return [{ text: line, kind: "plain" }];
  }
  const [, prefix, key, colon, rest] = match;
  const tokens: YamlToken[] = [];
  if (prefix) tokens.push({ text: prefix, kind: "plain" });
  tokens.push({ text: key, kind: "key" });
  tokens.push({ text: colon, kind: "plain" });
  if (rest) tokens.push({ text: rest, kind: "value" });
  return tokens;
}

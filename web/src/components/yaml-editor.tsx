import { yaml as yamlLang } from "@codemirror/lang-yaml";
import { EditorState } from "@codemirror/state";
import { EditorView, keymap, lineNumbers, highlightActiveLine } from "@codemirror/view";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { useEffect, useRef } from "react";

// A CodeMirror 6 YAML editor (Story 5.1). Kept deliberately small and isolated:
// it owns the editor instance and reports edits up via onChange, so the
// surrounding tab (apply/confirm/conflict logic) is plain React and testable
// without mounting CodeMirror. The initial value seeds the document; subsequent
// external value changes (e.g. a conflict reload) are reconciled below.

const editorExtensions = [
  lineNumbers(),
  highlightActiveLine(),
  history(),
  keymap.of([...defaultKeymap, ...historyKeymap, indentWithTab]),
  yamlLang(),
  EditorView.theme({
    "&": { fontSize: "12px", backgroundColor: "transparent" },
    ".cm-content": { fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace" },
    "&.cm-focused": { outline: "none" },
    ".cm-gutters": { backgroundColor: "transparent", border: "none" },
  }),
];

export function YamlEditor({
  value,
  onChange,
}: {
  value: string;
  onChange: (next: string) => void;
}) {
  const hostRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  // Mount once. The editor is uncontrolled after mount; the parent holds the
  // draft and only pushes a new value on an explicit reset (see the effect below).
  useEffect(() => {
    if (!hostRef.current) return;
    const view = new EditorView({
      parent: hostRef.current,
      state: EditorState.create({
        doc: value,
        extensions: [
          ...editorExtensions,
          EditorView.updateListener.of((update) => {
            if (update.docChanged) onChangeRef.current(update.state.doc.toString());
          }),
        ],
      }),
    });
    viewRef.current = view;
    return () => {
      view.destroy();
      viewRef.current = null;
    };
    // Mount-only: value is the initial seed; reconciliation is handled below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Reconcile an externally-changed value (a conflict reload replaces the draft
  // with the freshly-fetched manifest). Skip when the editor already matches, so
  // ordinary typing is never clobbered.
  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;
    if (view.state.doc.toString() === value) return;
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } });
  }, [value]);

  return (
    <div
      ref={hostRef}
      data-testid="yaml-editor"
      className="max-h-[32rem] overflow-auto rounded-md border bg-muted/40"
    />
  );
}

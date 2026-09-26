import Prism from "prismjs";
import "prismjs/components/prism-bash";
import "prismjs/components/prism-javascript";
import "prismjs/components/prism-json";
import "prismjs/components/prism-markdown";
import "prismjs/components/prism-python";
import "prismjs/components/prism-typescript";
import "prismjs/components/prism-yaml";
import { type KeyboardEvent, useId, useMemo, useRef, useState } from "react";

export interface CodeEditorProps {
  value: string;
  onChange: (value: string) => void;
  filePath: string;
  readOnly?: boolean;
  minHeight?: string;
  className?: string;
}

export function detectLanguage(path: string): {
  id: string;
  name: string;
  grammar: Prism.Grammar | undefined;
} {
  const lower = path.toLowerCase();
  const ext = lower.split(".").pop() ?? "";

  switch (ext) {
    case "py":
      return {
        grammar: Prism.languages.python,
        id: "python",
        name: "Python",
      };
    case "sh":
    case "bash":
    case "zsh":
      return {
        grammar: Prism.languages.bash,
        id: "bash",
        name: "Shell",
      };
    case "json":
      return {
        grammar: Prism.languages.json,
        id: "json",
        name: "JSON",
      };
    case "yaml":
    case "yml":
      return {
        grammar: Prism.languages.yaml,
        id: "yaml",
        name: "YAML",
      };
    case "md":
    case "markdown":
      return {
        grammar: Prism.languages.markdown,
        id: "markdown",
        name: "Markdown",
      };
    case "ts":
    case "tsx":
      return {
        grammar: Prism.languages.typescript,
        id: "typescript",
        name: "TypeScript",
      };
    case "js":
    case "jsx":
      return {
        grammar: Prism.languages.javascript,
        id: "javascript",
        name: "JavaScript",
      };
    default:
      return {
        grammar: undefined,
        id: "text",
        name: "Plain Text",
      };
  }
}

export function CodeEditor({
  value,
  onChange,
  filePath,
  readOnly = false,
  minHeight = "100%",
  className = "",
}: CodeEditorProps) {
  const [highlightEnabled, setHighlightEnabled] = useState(true);
  const preRef = useRef<HTMLPreElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const gutterRef = useRef<HTMLDivElement>(null);
  const editorId = useId();

  const lang = useMemo(() => detectLanguage(filePath), [filePath]);

  const lines = useMemo(() => {
    return value.split("\n");
  }, [value]);

  const highlightedHtml = useMemo(() => {
    if (!highlightEnabled || !lang.grammar) {
      return null;
    }
    try {
      // Ensure trailing newline is visible to prevent layout shift
      const safeText = value.endsWith("\n") ? `${value} ` : value;
      return Prism.highlight(safeText, lang.grammar, lang.id);
    } catch {
      return null;
    }
  }, [value, lang, highlightEnabled]);

  const handleScroll = (e: React.UIEvent<HTMLTextAreaElement>) => {
    const { scrollTop, scrollLeft } = e.currentTarget;
    if (preRef.current) {
      preRef.current.scrollTop = scrollTop;
      preRef.current.scrollLeft = scrollLeft;
    }
    if (gutterRef.current) {
      gutterRef.current.scrollTop = scrollTop;
    }
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Tab") {
      e.preventDefault();
      const target = e.currentTarget;
      const start = target.selectionStart;
      const end = target.selectionEnd;
      const spaces = "  ";
      const nextValue =
        value.substring(0, start) + spaces + value.substring(end);
      onChange(nextValue);
      requestAnimationFrame(() => {
        target.selectionStart = target.selectionEnd = start + spaces.length;
      });
    }
  };

  return (
    <div
      className={`flex h-full flex-col overflow-hidden bg-base-100 ${className}`}
      style={{ minHeight }}
    >
      {/* Editor top status bar */}
      <div className="flex items-center justify-between border-b border-base-content/10 bg-base-200/40 px-3 py-1 text-xs">
        <div className="flex items-center gap-2">
          <span className="badge badge-neutral badge-xs font-mono font-medium">
            {lang.name}
          </span>
          <span className="text-[10px] text-base-content/50 font-mono">
            {lines.length} lines • {value.length} chars
          </span>
        </div>

        <div className="flex items-center gap-2">
          <button
            className={`btn btn-ghost btn-xs h-6 min-h-0 text-[11px] ${
              highlightEnabled
                ? "text-primary font-medium"
                : "text-base-content/50"
            }`}
            onClick={() => setHighlightEnabled(!highlightEnabled)}
            title="Toggle live syntax highlighting"
            type="button"
          >
            {highlightEnabled ? "Syntax On" : "Syntax Off"}
          </button>
        </div>
      </div>

      {/* Editor body with line numbers gutter and overlay */}
      <div className="relative flex flex-1 overflow-hidden">
        {/* Line numbers gutter */}
        <div
          ref={gutterRef}
          aria-hidden="true"
          className="select-none overflow-hidden border-r border-base-content/10 bg-base-200/20 py-3 px-2 text-right font-mono text-xs leading-5 text-base-content/30"
          style={{ minWidth: "2.75rem" }}
        >
          {lines.map((_, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: line numbers are strictly index-bound
            <div key={i}>{i + 1}</div>
          ))}
        </div>

        {/* Text editing surface */}
        <div className="relative flex-1 overflow-hidden">
          {highlightedHtml ? (
            <pre
              ref={preRef}
              aria-hidden="true"
              className="pointer-events-none absolute inset-0 m-0 overflow-hidden whitespace-pre p-3 font-mono text-xs leading-5"
            >
              <code
                className={`language-${lang.id}`}
                // biome-ignore lint/security/noDangerouslySetInnerHtml: syntax highlighting HTML produced safely by Prism.js
                dangerouslySetInnerHTML={{ __html: highlightedHtml }}
              />
            </pre>
          ) : null}

          <textarea
            ref={textareaRef}
            aria-label="Code editor"
            className={`absolute inset-0 m-0 resize-none overflow-auto whitespace-pre p-3 font-mono text-xs leading-5 outline-none ${
              highlightedHtml
                ? "bg-transparent text-transparent caret-base-content selection:bg-primary/25"
                : "bg-base-100 text-base-content"
            }`}
            disabled={readOnly}
            id={editorId}
            onChange={(e) => onChange(e.target.value)}
            onKeyDown={handleKeyDown}
            onScroll={handleScroll}
            spellCheck={false}
            value={value}
          />
        </div>
      </div>
    </div>
  );
}

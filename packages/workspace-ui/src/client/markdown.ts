export type MarkdownBlock =
  | { kind: "heading"; level: 1 | 2 | 3; text: string }
  | { kind: "blockquote"; text: string }
  | { kind: "code"; text: string }
  | { kind: "list"; text: string }
  | { kind: "paragraph"; text: string };

export function markdownBlocks(value: string): MarkdownBlock[] {
  const lines = value.replace(/\r\n?/g, "\n").split("\n");
  const blocks: MarkdownBlock[] = [];
  const codeLines: string[] = [];
  let inCode = false;

  for (const line of lines) {
    if (line.startsWith("~~~")) {
      if (inCode) {
        blocks.push({ kind: "code", text: codeLines.join("\n") });
        codeLines.length = 0;
        inCode = false;
      } else {
        inCode = true;
      }
      continue;
    }
    if (inCode) {
      codeLines.push(line);
      continue;
    }
    if (line.startsWith("### "))
      blocks.push({ kind: "heading", level: 3, text: line.slice(4) });
    else if (line.startsWith("## "))
      blocks.push({ kind: "heading", level: 2, text: line.slice(3) });
    else if (line.startsWith("# "))
      blocks.push({ kind: "heading", level: 1, text: line.slice(2) });
    else if (line.startsWith("> "))
      blocks.push({ kind: "blockquote", text: line.slice(2) });
    else if (/^[-*] /.test(line))
      blocks.push({ kind: "list", text: line.slice(2) });
    else if (line.trim()) blocks.push({ kind: "paragraph", text: line });
  }
  if (inCode) blocks.push({ kind: "code", text: codeLines.join("\n") });
  return blocks;
}

export function diffLines(original: string, draft: string): string[] {
  const before = original.split("\n");
  const after = draft.split("\n");
  const rows: string[] = [];
  const length = Math.max(before.length, after.length);
  for (let index = 0; index < length; index += 1) {
    const previous = before[index];
    const next = after[index];
    if (previous === next) {
      if (previous !== undefined) rows.push(`  ${previous}`);
      continue;
    }
    if (previous !== undefined) rows.push(`- ${previous}`);
    if (next !== undefined) rows.push(`+ ${next}`);
  }
  return rows;
}

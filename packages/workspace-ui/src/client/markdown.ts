export function diffLines(original: string, draft: string): string[] {
  if (original === draft) return [];

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

export interface ControlCharacterReplacement {
  character: string;
  count: number;
}

export interface SanitizedText {
  value: string;
  replacements: ControlCharacterReplacement[];
  replacementCount: number;
}

const FORBIDDEN_CONTROL_CHARACTER = /[\u0000-\u0008\u000B\u000C\u000E-\u001F]/gu;

export function sanitizeControlCharacters(value: string): SanitizedText {
  const counts = new Map<number, number>();
  const sanitized = value.replace(FORBIDDEN_CONTROL_CHARACTER, (character) => {
    const codePoint = character.codePointAt(0);
    if (codePoint !== undefined) counts.set(codePoint, (counts.get(codePoint) ?? 0) + 1);
    return " ";
  });
  const replacements = [...counts.entries()]
    .sort(([left], [right]) => left - right)
    .map(([codePoint, count]) => ({
      character: formatCodePoint(codePoint),
      count,
    }));
  return {
    value: sanitized,
    replacements,
    replacementCount: replacements.reduce((sum, item) => sum + item.count, 0),
  };
}

export function containsForbiddenControlCharacters(value: string): boolean {
  FORBIDDEN_CONTROL_CHARACTER.lastIndex = 0;
  return FORBIDDEN_CONTROL_CHARACTER.test(value);
}

function formatCodePoint(codePoint: number): string {
  return `U+${codePoint.toString(16).toUpperCase().padStart(4, "0")}`;
}

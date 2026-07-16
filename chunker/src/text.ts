export interface ControlCharacterReplacement {
  character: string;
  count: number;
}

const FORBIDDEN_CONTROL_CHARACTER = /[\u0000-\u0008\u000B\u000C\u000E-\u001F]/gu;

export function sanitizeControlCharacters(value: string): {
  value: string;
  replacements: ControlCharacterReplacement[];
} {
  const counts = new Map<number, number>();
  const sanitized = value.replace(FORBIDDEN_CONTROL_CHARACTER, (character) => {
    const codePoint = character.codePointAt(0);
    if (codePoint !== undefined) counts.set(codePoint, (counts.get(codePoint) ?? 0) + 1);
    return " ";
  });
  return {
    value: sanitized,
    replacements: [...counts.entries()]
      .sort(([left], [right]) => left - right)
      .map(([codePoint, count]) => ({
        character: `U+${codePoint.toString(16).toUpperCase().padStart(4, "0")}`,
        count,
      })),
  };
}

export function containsForbiddenControlCharacters(value: string): boolean {
  FORBIDDEN_CONTROL_CHARACTER.lastIndex = 0;
  return FORBIDDEN_CONTROL_CHARACTER.test(value);
}

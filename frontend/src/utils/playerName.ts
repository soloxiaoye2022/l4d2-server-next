const colorTagPattern = /<#(?:[0-9a-f]{3}|[0-9a-f]{4}|[0-9a-f]{6}|[0-9a-f]{8})>/gi;
const hiddenCodePoints = new Set([
  0x00ad,
  0x034f,
  0x061c,
  0x115f,
  0x1160,
  0x17b4,
  0x17b5,
  0x180e,
  0x200b,
  0x200e,
  0x200f,
  0x202a,
  0x202b,
  0x202c,
  0x202d,
  0x202e,
  0x2060,
  0x2061,
  0x2062,
  0x2063,
  0x2064,
  0x2066,
  0x2067,
  0x2068,
  0x2069,
  0x2800,
  0x3164,
  0xfeff,
  0xffa0,
]);

export function sanitizePlayerName(value: unknown): string {
  if (value === null || value === undefined) return '';
  const withoutTags = String(value).replace(colorTagPattern, '');
  const withoutHidden = Array.from(withoutTags)
    .filter((char) => {
      const codePoint = char.codePointAt(0) || 0;
      if (codePoint <= 0x1f || (codePoint >= 0x7f && codePoint <= 0x9f)) return false;
      return !hiddenCodePoints.has(codePoint);
    })
    .join('');
  return withoutHidden.trim().replace(/\s+/g, ' ');
}

export function displayPlayerName(value: unknown, fallback = 'Unknown'): string {
  return sanitizePlayerName(value) || fallback;
}

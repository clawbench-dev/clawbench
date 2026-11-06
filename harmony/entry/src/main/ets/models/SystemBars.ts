export function normalizeBarColor(value: string): string {
  if (!/^#[0-9a-f]{6}$/i.test(value)) {
    throw new Error('Invalid system bar color');
  }
  return value.toUpperCase();
}

export function barContentColor(background: string): string {
  const color = normalizeBarColor(background).slice(1);
  const channels = [0, 2, 4].map((offset: number) => {
    const value = parseInt(color.slice(offset, offset + 2), 16) / 255;
    return value <= 0.04045 ? value / 12.92 : Math.pow((value + 0.055) / 1.055, 2.4);
  });
  const luminance = 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
  return luminance > 0.179 ? '#000000' : '#FFFFFF';
}

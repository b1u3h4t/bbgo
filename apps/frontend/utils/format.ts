export function fmtNum(value: number | null | undefined, digits = 2): string {
  if (value == null || Number.isNaN(value)) {
    return '0';
  }
  return Number(value).toLocaleString('en-US', {
    minimumFractionDigits: 0,
    maximumFractionDigits: digits,
  });
}

export function fmtPct(value: number | null | undefined, digits = 2): string {
  if (value == null || Number.isNaN(value) || !Number.isFinite(value)) {
    return '0%';
  }
  return `${Number(value).toFixed(digits)}%`;
}

export function fmtPrice(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value)) {
    return '-';
  }
  const abs = Math.abs(value);
  if (abs >= 1000) return fmtNum(value, 2);
  if (abs >= 1) return fmtNum(value, 4);
  return fmtNum(value, 6);
}

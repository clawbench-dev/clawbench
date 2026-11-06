export interface SafeInsets {
  top: number;
  bottom: number;
  left: number;
  right: number;
}

export function mergeSafeInsets(areas: SafeInsets[], keyboardVisible: boolean): SafeInsets {
  const result: SafeInsets = { top: 0, bottom: 0, left: 0, right: 0 };
  for (const area of areas) {
    result.top = Math.max(result.top, area.top);
    result.bottom = Math.max(result.bottom, area.bottom);
    result.left = Math.max(result.left, area.left);
    result.right = Math.max(result.right, area.right);
  }
  // ArkUI RESIZE already removes the keyboard area; do not reserve the gesture bar again.
  if (keyboardVisible) {
    result.bottom = 0;
  }
  return result;
}

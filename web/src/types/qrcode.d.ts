// Ambient declaration for the `qrcode` package.
//
// The dependency is declared in web/package.json but ships no type definitions
// and has no @types/qrcode, so a plain import fails `vue-tsc --noEmit` under
// strict mode. Only the two entry points this app uses are declared; adding more
// here later is cheap, and no extra dependency is installed.
declare module 'qrcode' {
  interface QRCodeToDataURLOptions {
    width?: number
    margin?: number
    errorCorrectionLevel?: 'L' | 'M' | 'Q' | 'H'
    type?: 'image/png' | 'image/jpeg' | 'image/webp'
    color?: { dark?: string; light?: string }
  }

  /** Render `text` as a data URL (default: PNG). */
  export function toDataURL(text: string, options?: QRCodeToDataURLOptions): Promise<string>

  /** Render `text` as an SVG string. */
  export function toString(text: string, options?: QRCodeToDataURLOptions): Promise<string>
}

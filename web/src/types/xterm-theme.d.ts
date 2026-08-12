declare module 'xterm-theme' {
  export interface XtermTheme {
    foreground: string
    background: string
    cursor?: string
    cursorAccent?: string
    black: string
    brightBlack: string
    red: string
    brightRed: string
    green: string
    brightGreen: string
    yellow: string
    brightYellow: string
    blue: string
    brightBlue: string
    magenta: string
    brightMagenta: string
    cyan: string
    brightCyan: string
    white: string
    brightWhite: string
  }

  export const AdventureTime: XtermTheme
  export const Dracula: XtermTheme
  const themes: Record<string, XtermTheme>
  export default themes
}

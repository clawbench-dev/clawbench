export const APP_VERSION = '0.2.4';

export function isLocalLoginPage(url: string): boolean {
  return /^resource:\/\/rawfile\/login\/index\.html(?:[?#].*)?$/i.test(url);
}

export const APP_VERSION = '0.2.3';

export function isLocalLoginPage(url: string): boolean {
  return /^resource:\/\/rawfile\/login\/index\.html(?:[?#].*)?$/i.test(url);
}

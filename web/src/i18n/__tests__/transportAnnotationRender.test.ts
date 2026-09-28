import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

/**
 * Rendered-output guard for the transport annotation across the real locales.
 *
 * The composable/component tests run against hand-written message tables and an
 * identity `gt` mock, so they prove the wiring but not the actual copy. This
 * pins the real zh/en strings: the annotation appends inside the sentence and
 * the neutral wording is byte-identical to the pre-annotation baseline when no
 * single wire is known.
 */

const i18n = createI18n({ legacy: false, locale: 'zh', fallbackLocale: 'zh', messages: { zh, en } })

function render(locale: 'zh' | 'en', transport: '' | 'ssh' | 'h2') {
  i18n.global.locale.value = locale
  const t = i18n.global.t as (k: string, p?: Record<string, unknown>) => string
  const labelKey = transport === 'ssh' ? 'proxy.transportSsh'
    : transport === 'h2' ? 'proxy.transportH2'
      : ''
  const ann = labelKey ? t('proxy.transportAnnotation', { transport: t(labelKey) }) : ''
  return (key: string) => t(key, { transport: ann })
}

describe('transport annotation rendered copy', () => {
  it('appends （HTTP/2） mid-sentence in zh', () => {
    const t = render('zh', 'h2')
    expect(t('proxy.tunnelDisconnected')).toBe('隧道未连接（HTTP/2）')
    expect(t('proxy.tunnelConnectedButNoResponse')).toBe('隧道已连接（HTTP/2），但所有端口的服务均未响应')
    expect(t('portForward.tunnelReconnected')).toBe('隧道已重连（HTTP/2）')
    expect(t('portForward.tunnelDisconnected')).toBe('隧道未连接（HTTP/2），端口映射将无法使用')
    expect(t('proxy.toast.tunnelStillDisconnected')).toBe('隧道仍未连接（HTTP/2）')
  })

  it('appends （SSH） mid-sentence in zh', () => {
    const t = render('zh', 'ssh')
    expect(t('proxy.tunnelDisconnected')).toBe('隧道未连接（SSH）')
    expect(t('portForward.tunnelDegraded')).toBe('隧道已连接（SSH），但所有转发端口均无服务响应')
    expect(t('proxy.backgroundTip')).toBe('使用外部浏览器时，请确保已授予应用后台运行权限，否则 APP 进入后台后隧道会被系统终止（SSH）')
  })

  it('uses half-width brackets in en', () => {
    const t = render('en', 'ssh')
    expect(t('proxy.tunnelDisconnected')).toBe('Tunnel disconnected (SSH)')
    expect(t('portForward.tunnelReconnected')).toBe('Tunnel reconnected (SSH)')
    const h2 = render('en', 'h2')
    expect(h2('proxy.tunnelConnectedButNoResponse')).toBe('Tunnel connected (HTTP/2), but all port services are unresponsive')
  })

  it('renders the neutral wording unchanged when no single wire is known', () => {
    for (const locale of ['zh', 'en'] as const) {
      const t = render(locale, '')
      for (const key of [
        'proxy.tunnelDisconnected', 'proxy.tunnelConnectedButNoResponse', 'proxy.backgroundTip',
        'proxy.toast.tunnelRecovered', 'proxy.toast.tunnelConnectedNoResponse', 'proxy.toast.tunnelStillDisconnected',
        'portForward.tunnelDegraded', 'portForward.tunnelDisconnected', 'portForward.tunnelReconnected',
      ]) {
        const text = t(key)
        expect(text, `${locale}:${key}`).not.toMatch(/[（(]/)
        expect(text, `${locale}:${key}`).not.toMatch(/SSH|HTTP\/2/)
      }
    }
  })
})

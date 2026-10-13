import { useI18n } from '../i18n'
import { useTheme } from '../theme'

export function PreferenceControls() {
  const { language, setLanguage, t } = useI18n()
  const { theme, setTheme, visualOverride, setVisualOverride } = useTheme()
  const nextTheme = theme === 'dark' ? 'light' : 'dark'
  return (
    <div className="preference-controls">
      <button type="button" aria-label={t('languageSwitchLabel')} onClick={() => setLanguage(language === 'en' ? 'zh' : 'en')}>{t('languageSwitch')}</button>
      <button type="button" aria-label={t(nextTheme === 'light' ? 'themeSwitchToLight' : 'themeSwitchToDark')} onClick={() => setTheme(nextTheme)}>{t(nextTheme === 'light' ? 'themeLight' : 'themeDark')}</button>
      <select aria-label={t('browserTheme')} value={visualOverride ?? 'site'} onChange={(event) => {
        const value = event.target.value
        if (value === 'site') setVisualOverride(null)
        else if (value === 'emerald' || value === 'classic') setVisualOverride(value)
      }}><option value="site">{t('followSite')}</option><option value="emerald">Emerald</option><option value="classic">Classic</option></select>
    </div>
  )
}

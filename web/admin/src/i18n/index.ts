import { ref, computed, watchEffect } from 'vue'
import zh from './zh'
import en from './en'

type Locale = 'zh' | 'en'
type Messages = typeof zh

const messages: Record<Locale, Messages> = {
  zh,
  en
}

const LOCALE_KEY = 'fenfa_admin_locale'

function isLocale(value: unknown): value is Locale {
  return value === 'en' || value === 'zh'
}

function initialLocale(): Locale {
  try {
    const savedLocale = localStorage.getItem(LOCALE_KEY)
    if (isLocale(savedLocale)) return savedLocale
  } catch {
    // Browser preferences still work when storage is unavailable.
  }

  for (const language of [...(navigator.languages || []), navigator.language]) {
    const locale = language?.toLowerCase().split('-')[0]
    if (isLocale(locale)) return locale
  }
  return 'en'
}

const currentLocale = ref<Locale>(initialLocale())

watchEffect(() => {
  document.documentElement.lang = currentLocale.value
})

export function useI18n() {
  const locale = computed(() => currentLocale.value)
  
  const t = computed(() => messages[currentLocale.value])
  
  function setLocale(newLocale: Locale) {
    currentLocale.value = newLocale
    try {
      localStorage.setItem(LOCALE_KEY, newLocale)
    } catch (e) {
      console.warn('Failed to save locale:', e)
    }
  }
  
  return {
    locale,
    t,
    setLocale
  }
}


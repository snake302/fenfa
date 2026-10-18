import { computed } from 'vue'

function detectLanguage(): 'zh' | 'en' {
  return navigator.language.toLowerCase().startsWith('zh') ? 'zh' : 'en'
}

const translations = {
  zh: {
    loading: '加载中...',
    auth: {
      title: '登录',
      subtitle: '输入访问密码以查看和安装产品。',
      password: '访问密码',
      signIn: '登录',
      signingIn: '登录中...',
      logout: '退出登录',
      invalidPassword: '密码错误，请重试。',
      rateLimited: '尝试次数过多，请稍后重试。',
      loginFailed: '无法登录，请重试。',
      logoutFailed: '无法退出登录，请重试。'
    },
    catalog: {
      title: '所有产品',
      subtitle: '选择产品以查看可用版本并安装到您的设备。',
      empty: '暂无已发布的产品。'
    },
    error: {
      title: '页面暂时不可用',
      loadFailed: '加载产品信息失败'
    },
    actions: {
      allProducts: '所有产品',
      view: '查看',
      retry: '重试',
      install: '安装',
      installing: '正在安装...',
      download: '下载',
      bindDevice: '绑定设备',
      scan: '扫码安装',
      unavailable: '暂不可用'
    },
    stats: {
      date: '日期',
      version: '版本',
      build: '构建',
      size: '大小',
      download: '下载',
      requires: '要求',
      format: '格式'
    },
    sections: {
      whatsNew: '新内容',
      previousBuilds: '历史构建',
      otherBuilds: '其他构建',
      information: '信息',
      noChangelog: '此构建没有更新说明。'
    },
    info: {
      identifier: '标识',
      platform: '平台',
      architecture: '架构',
      channel: '渠道',
      downloads: '下载次数'
    },
    variant: {
      version: '版本',
      iosHint: 'iOS 首次安装前需要先绑定当前设备 UDID。',
      safariHint: '请在 Safari 中打开此页面以安装应用。',
      iosFooter: '只有在签名 Apple 账号中注册过的设备才能安装 iOS 构建。'
    },
    time: {
      justNow: '刚刚',
      minutesAgo: '分钟前',
      hoursAgo: '小时前',
      daysAgo: '天前',
      weeksAgo: '周前',
      monthsAgo: '个月前'
    }
  },
  en: {
    loading: 'Loading...',
    auth: {
      title: 'Sign in',
      subtitle: 'Enter the access password to browse and install products.',
      password: 'Access password',
      signIn: 'Sign in',
      signingIn: 'Signing in...',
      logout: 'Sign out',
      invalidPassword: 'Incorrect password. Please try again.',
      rateLimited: 'Too many attempts. Please wait a minute and try again.',
      loginFailed: 'Unable to sign in. Please try again.',
      logoutFailed: 'Unable to sign out. Please try again.'
    },
    catalog: {
      title: 'All products',
      subtitle: 'Choose a product to view available versions and install it on your device.',
      empty: 'No products have been published yet.'
    },
    error: {
      title: 'This page is temporarily unavailable',
      loadFailed: 'Failed to load product information'
    },
    actions: {
      allProducts: 'All products',
      view: 'View',
      retry: 'Try again',
      install: 'Install',
      installing: 'Installing...',
      download: 'Download',
      bindDevice: 'Register Device',
      scan: 'Scan to install',
      unavailable: 'Unavailable'
    },
    stats: {
      date: 'Date',
      version: 'Version',
      build: 'Build',
      size: 'Size',
      download: 'download',
      requires: 'Requires',
      format: 'Format'
    },
    sections: {
      whatsNew: "What's New",
      previousBuilds: 'Previous builds',
      otherBuilds: 'Other builds',
      information: 'Information',
      noChangelog: 'No release notes for this build.'
    },
    info: {
      identifier: 'Identifier',
      platform: 'Platform',
      architecture: 'Architecture',
      channel: 'Channel',
      downloads: 'Downloads'
    },
    variant: {
      version: 'Version',
      iosHint: 'Register this iPhone once before the first install.',
      safariHint: 'Open this page in Safari to install. Other browsers cannot install apps.',
      iosFooter: 'iOS builds install only on devices registered to the signing Apple account.'
    },
    time: {
      justNow: 'Just now',
      minutesAgo: 'min ago',
      hoursAgo: 'h ago',
      daysAgo: 'd ago',
      weeksAgo: 'w ago',
      monthsAgo: 'mo ago'
    }
  }
}

export type Language = 'zh' | 'en'

let currentLanguage: Language = detectLanguage()

export function useI18n() {
  const t = computed(() => translations[currentLanguage])

  const setLanguage = (lang: Language) => {
    currentLanguage = lang
  }

  const getLanguage = () => currentLanguage

  return { t, setLanguage, getLanguage }
}

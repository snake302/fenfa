import { computed, createApp, nextTick, onMounted, onUnmounted, reactive, ref } from 'vue'
import QRCode from 'qrcode'
import { useI18n } from './i18n'
import HomePage from './HomePage'
import { platformLabel } from './platform'
import { defaultTint, icons, sampleIconTint } from './chrome'
import './style.css'

type Platform = 'ios' | 'android' | 'macos' | 'windows' | 'linux'

interface ReleaseActions {
  download?: string
  release_page?: string
  ios_manifest?: string
  ios_install?: string
}

interface ReleaseItem {
  id: string
  version: string
  build?: number
  created_at: string
  download_count?: number
  channel?: string
  min_os?: string
  changelog?: string
  size_bytes?: number
  actions: ReleaseActions
}

interface VariantItem {
  id: string
  platform: Platform
  identifier: string
  display_name?: string
  arch?: string
  installer_type?: string
  min_os?: string
  latest_release?: ReleaseItem | null
  releases: ReleaseItem[]
}

interface ProductData {
  product: {
    id: string
    slug?: string
    name: string
    icon_url?: string
  }
  variants: VariantItem[]
  disable_bind_device?: boolean
}

function detectClientPlatform(): Platform | '' {
  const ua = navigator.userAgent.toLowerCase()
  if (/iphone|ipad|ipod/.test(ua)) return 'ios'
  if (ua.includes('android')) return 'android'
  if (ua.includes('mac os x') || ua.includes('macintosh')) return 'macos'
  if (ua.includes('windows')) return 'windows'
  if (ua.includes('linux')) return 'linux'
  return ''
}

const App = {
  setup() {
    const { t, getLanguage } = useI18n()
    const productData = ref<ProductData | null>(null)
    const loading = ref(true)
    const error = ref('')
    const installing = ref(false)
    const udidBound = reactive<Record<string, boolean>>({})
    const disableBindDevice = computed(() => productData.value?.disable_bind_device === true)
    const selectedReleaseByVariant = reactive<Record<string, ReleaseItem | null>>({})
    const clientPlatform = detectClientPlatform()
    const pathParts = location.pathname.split('/').filter(Boolean)
    const selectedVariantID = ref('')
    const heroTint = ref(defaultTint)
    const isIOSNonSafari = clientPlatform === 'ios' && /crios|fxios|edgios|opios|gsa\/|instagram|fban|fbav|line\/|micromessenger/i.test(navigator.userAgent)
    const resourceID = pathParts[1] || ''
    let refreshInFlight: Promise<void> | null = null

    const sortedVariants = computed(() => {
      const items = [...(productData.value?.variants || [])]
      return items.sort((left, right) => {
        const leftScore = left.platform === clientPlatform ? 1 : 0
        const rightScore = right.platform === clientPlatform ? 1 : 0
        if (leftScore !== rightScore) return rightScore - leftScore
        return left.platform.localeCompare(right.platform)
      })
    })

    const recommendedVariant = computed(() => {
      return sortedVariants.value.find((variant) => variant.platform === clientPlatform) || sortedVariants.value[0] || null
    })

    const currentVariant = computed(() => {
      return sortedVariants.value.find((variant) => variant.id === selectedVariantID.value) || recommendedVariant.value
    })

    const currentRelease = computed(() => currentVariant.value ? getSelectedRelease(currentVariant.value) : null)

    const otherReleases = computed(() => {
      const variant = currentVariant.value
      if (!variant) return []
      return variant.releases.filter((release) => release.id !== currentRelease.value?.id)
    })

    const showingLatest = computed(() => {
      const variant = currentVariant.value
      return !variant || currentRelease.value?.id === (variant.latest_release || variant.releases[0])?.id
    })

    function selectVariant(variant: VariantItem) {
      selectedVariantID.value = variant.id
    }

    function getSelectedRelease(variant: VariantItem): ReleaseItem | null {
      return selectedReleaseByVariant[variant.id] || variant.latest_release || variant.releases[0] || null
    }

    function getVariantAction(variant: VariantItem, release: ReleaseItem | null) {
      if (!release) return { type: 'disabled', href: '#', label: t.value.actions.unavailable }
      if (variant.platform === 'ios') {
        if (disableBindDevice.value || udidBound[variant.id]) {
          return {
            type: 'install',
            href: release.actions?.ios_install || '',
            label: installing.value ? t.value.actions.installing : t.value.actions.install
          }
        }
        return {
          type: 'bind',
          href: `/udid/profile.mobileconfig?variant=${encodeURIComponent(variant.id)}`,
          label: t.value.actions.bindDevice
        }
      }
      return {
        type: 'download',
        href: release.actions?.download || '',
        label: t.value.actions.download
      }
    }

    function getCookie(name: string): string | null {
      const match = document.cookie.match(new RegExp('(^| )' + name + '=([^;]+)'))
      return match ? match[2] : null
    }

    async function fetchUDIDStatuses() {
      if (disableBindDevice.value) return
      const variants = productData.value?.variants || []
      const iosVariants = variants.filter((variant) => variant.platform === 'ios')
      if (iosVariants.length === 0) return

      const urlParams = new URLSearchParams(location.search)
      if (urlParams.get('udid_bound') === '1') {
        const boundVariantID = urlParams.get('variant')
        if (boundVariantID) {
          udidBound[boundVariantID] = true
        } else {
          iosVariants.forEach((variant) => {
            udidBound[variant.id] = true
          })
        }
        urlParams.delete('udid_bound')
        urlParams.delete('variant')
        const nextURL = urlParams.toString() ? `${location.pathname}?${urlParams}` : location.pathname
        history.replaceState(null, '', nextURL)
        return
      }

      const nonce = getCookie('udid_nonce')
      await Promise.all(iosVariants.map(async (variant) => {
        try {
          const params = new URLSearchParams()
          if (nonce) params.set('nonce', nonce)
          params.set('variant', variant.id)
          const res = await fetch(`/udid/status?${params}`, { cache: 'no-store' })
          const payload = await res.json()
          udidBound[variant.id] = !!payload?.data?.bound
        } catch {
          udidBound[variant.id] = false
        }
      }))
    }

    async function fetchData() {
      const selectedReleaseID = new URLSearchParams(location.search).get('r')
      const endpoint = `/api/products/${resourceID}`
      const res = await fetch(endpoint + (selectedReleaseID ? `?r=${encodeURIComponent(selectedReleaseID)}` : ''), { cache: 'no-store' })
      if (res.status === 401) {
        location.replace('/')
        return
      }
      const payload = await res.json()

      if (!res.ok || !payload.ok) {
        loading.value = false
        error.value = payload.error?.message || t.value.error.loadFailed
        return
      }

      const previousIconURL = productData.value?.product.icon_url
      productData.value = payload.data as ProductData
      error.value = ''
      document.title = productData.value.product.name
      const iconURL = productData.value.product.icon_url || ''
      if (iconURL !== previousIconURL) {
        sampleIconTint(iconURL).then((tint) => { heroTint.value = tint })
      }

      for (const variant of productData.value.variants) {
        const preferredID = selectedReleaseByVariant[variant.id]?.id || selectedReleaseID
        const preferred = variant.releases.find((release) => release.id === preferredID) || variant.latest_release || variant.releases[0] || null
        selectedReleaseByVariant[variant.id] = preferred
      }

      loading.value = false
      await nextTick()
      await renderQRCode()
      await fetchUDIDStatuses()
    }

    function refreshProduct(): Promise<void> {
      if (!refreshInFlight) {
        refreshInFlight = fetchData().catch(() => {
          if (!productData.value) {
            error.value = t.value.error.loadFailed
            loading.value = false
          }
        }).finally(() => { refreshInFlight = null })
      }
      return refreshInFlight
    }

    function handleVisibilityChange() {
      if (document.visibilityState === 'visible') void refreshProduct()
    }

    function handlePageShow(event: PageTransitionEvent) {
      if (event.persisted) void refreshProduct()
    }

    async function renderQRCode() {
      try {
        const canvas = document.getElementById('qrCanvas') as HTMLCanvasElement | null
        if (!canvas) return
        await QRCode.toCanvas(canvas, location.href, { width: 132, margin: 1 })
      } catch (err) {
        console.warn('qr failed', err)
      }
    }

    function sendBeaconJSON(url: string, payload: any) {
      try {
        if (navigator.sendBeacon) {
          const blob = new Blob([JSON.stringify(payload)], { type: 'application/json' })
          navigator.sendBeacon(url, blob)
          return
        }
      } catch {}
      fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      }).catch(() => {})
    }

    async function logVisit() {
      const variant = recommendedVariant.value
      const release = variant ? getSelectedRelease(variant) : null
      if (!variant || !release) return

      try {
        const mod: any = await import('ua-parser-js')
        const UAParser = mod.UAParser || mod.default
        const parser = new UAParser(navigator.userAgent)
        sendBeaconJSON('/events', {
          type: 'visit',
          variant_id: variant.id,
          release_id: release.id,
          extra: {
            product_id: productData.value?.product.id || '',
            variant_id: variant.id,
            ua: parser.getResult(),
            client_platform: clientPlatform,
            path: location.pathname
          }
        })
      } catch {}
    }

    function trackAction(variant: VariantItem, release: ReleaseItem | null, href: string) {
      if (!release || !href) return
      sendBeaconJSON('/events', {
        type: 'click',
        variant_id: variant.id,
        release_id: release.id,
        extra: {
          product_id: productData.value?.product.id || '',
          variant_id: variant.id,
          href,
          path: location.pathname
        }
      })
    }

    function handleAction(variant: VariantItem, release: ReleaseItem | null) {
      const action = getVariantAction(variant, release)
      if (!release || !action.href) return
      if (action.type === 'install') {
        installing.value = true
        trackAction(variant, release, action.href)
        location.href = action.href
        setTimeout(() => { installing.value = false }, 10000)
        return
      }
      if (action.type === 'bind') {
        trackAction(variant, release, action.href)
        location.href = action.href
        return
      }
      trackAction(variant, release, action.href)
      location.href = action.href
    }

    function formatTime(dateStr: string) {
      const date = new Date(dateStr)
      const now = new Date()
      const diff = now.getTime() - date.getTime()
      const minutes = Math.floor(diff / (1000 * 60))
      const hours = Math.floor(diff / (1000 * 60 * 60))
      const days = Math.floor(diff / (1000 * 60 * 60 * 24))

      if (minutes < 1) return t.value.time.justNow
      if (minutes < 60) return `${minutes} ${t.value.time.minutesAgo}`
      if (hours < 24) return `${hours} ${t.value.time.hoursAgo}`
      if (days < 7) return `${days} ${t.value.time.daysAgo}`
      if (days < 30) return `${Math.floor(days / 7)} ${t.value.time.weeksAgo}`
      if (days < 365) return `${Math.floor(days / 30)} ${t.value.time.monthsAgo}`
      return date.toLocaleDateString()
    }

    function locale() {
      return getLanguage() === 'zh' ? 'zh-CN' : 'en-US'
    }

    function formatDay(dateStr: string) {
      return new Date(dateStr).toLocaleDateString(locale(), { month: 'short', day: 'numeric' })
    }

    function formatClock(dateStr: string) {
      return new Date(dateStr).toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' })
    }

    function formatFullDate(dateStr: string) {
      return new Date(dateStr).toLocaleString(locale())
    }

    function formatSize(bytes?: number) {
      if (!bytes || bytes <= 0) return ''
      if (bytes < 1024 * 1024) return `${Math.max(1, Math.round(bytes / 1024))} KB`
      if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
      return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`
    }

    function versionLabel(release: ReleaseItem) {
      return release.build ? `${release.version} (${release.build})` : release.version
    }

    function packageFormat(variant: VariantItem) {
      const defaults: Record<string, string> = { ios: 'IPA', android: 'APK' }
      return (variant.installer_type || defaults[variant.platform] || '').toUpperCase()
    }

    function shortActionLabel(variant: VariantItem, release: ReleaseItem) {
      const action = getVariantAction(variant, release)
      return action.type === 'download' ? t.value.actions.download : t.value.actions.install
    }

    onMounted(async () => {
      document.addEventListener('visibilitychange', handleVisibilityChange)
      window.addEventListener('pageshow', handlePageShow)
      await refreshProduct()
      await logVisit()
    })

    onUnmounted(() => {
      document.removeEventListener('visibilitychange', handleVisibilityChange)
      window.removeEventListener('pageshow', handlePageShow)
    })

    return {
      currentRelease,
      currentVariant,
      error,
      formatClock,
      formatDay,
      formatFullDate,
      formatSize,
      formatTime,
      getVariantAction,
      handleAction,
      heroTint,
      icons,
      isIOSNonSafari,
      loading,
      otherReleases,
      packageFormat,
      platformLabel,
      productData,
      selectVariant,
      shortActionLabel,
      showingLatest,
      sortedVariants,
      t,
      versionLabel
    }
  },
  template: /* html */ `
  <div class="sf-page">
    <header class="sf-header">
      <div class="sf-header-inner">
        <a href="/" class="sf-round-button" :aria-label="t.actions.allProducts" :title="t.actions.allProducts" v-html="icons.back"></a>
        <a href="/" class="sf-wordmark" aria-label="Fenfa"><span>fen</span><span>fa</span></a>
        <span class="sf-header-spacer" aria-hidden="true"></span>
      </div>
    </header>

    <main v-if="loading" class="sf-main sf-state" role="status">
      <div class="sf-spinner"></div>
      <p>{{ t.loading }}</p>
    </main>

    <main v-else-if="error" class="sf-main sf-state" role="alert">
      <h1>{{ t.error.title }}</h1>
      <p>{{ error }}</p>
    </main>

    <main v-else-if="productData" class="sf-main">
      <section class="sf-hero">
        <div class="sf-hero-tint" aria-hidden="true" :style="{ background: heroTint }"></div>
        <div class="sf-hero-identity">
          <img v-if="productData.product.icon_url" :src="productData.product.icon_url" alt="" class="sf-app-icon" width="128" height="128" />
          <div v-else class="sf-app-icon sf-app-icon-fallback" aria-hidden="true">{{ productData.product.name.slice(0, 1).toUpperCase() }}</div>
          <h1 class="sf-app-name">{{ productData.product.name }}</h1>
        </div>

        <div v-if="sortedVariants.length > 1" class="sf-platforms" role="tablist">
          <button
            v-for="variant in sortedVariants"
            :key="variant.id"
            type="button"
            role="tab"
            class="sf-platform"
            :class="{ active: currentVariant?.id === variant.id }"
            :aria-selected="currentVariant?.id === variant.id"
            @click="selectVariant(variant)"
          >{{ platformLabel(variant.platform) }}</button>
        </div>

        <div v-if="currentVariant" class="sf-hero-actions">
          <button
            type="button"
            class="sf-glass-button"
            :disabled="getVariantAction(currentVariant, currentRelease).type === 'disabled'"
            @click="handleAction(currentVariant, currentRelease)"
          >
            <span v-html="getVariantAction(currentVariant, currentRelease).type === 'bind' ? icons.device : icons.download"></span>
            {{ getVariantAction(currentVariant, currentRelease).label }}
          </button>
          <p v-if="getVariantAction(currentVariant, currentRelease).type === 'bind'" class="sf-hint">{{ t.variant.iosHint }}</p>
          <p v-else-if="currentVariant.platform === 'ios' && isIOSNonSafari" class="sf-hint">{{ t.variant.safariHint }}</p>
        </div>
      </section>

      <template v-if="currentVariant && currentRelease">
        <div class="sf-stats-scroll">
          <dl class="sf-stats">
            <div class="sf-stat">
              <dt>{{ t.stats.date }}</dt>
              <dd>{{ formatDay(currentRelease.created_at) }}</dd>
              <dd class="sf-stat-sub">{{ formatClock(currentRelease.created_at) }}</dd>
            </div>
            <div class="sf-stat">
              <dt>{{ t.stats.version }}</dt>
              <dd>{{ currentRelease.version }}</dd>
              <dd class="sf-stat-sub">{{ currentRelease.build ? t.stats.build + ' ' + currentRelease.build : (currentRelease.channel || platformLabel(currentVariant.platform)) }}</dd>
            </div>
            <div v-if="formatSize(currentRelease.size_bytes)" class="sf-stat">
              <dt>{{ t.stats.size }}</dt>
              <dd>{{ formatSize(currentRelease.size_bytes) }}</dd>
              <dd class="sf-stat-sub">{{ t.stats.download }}</dd>
            </div>
            <div v-if="currentRelease.min_os || currentVariant.min_os" class="sf-stat">
              <dt>{{ t.stats.requires }}</dt>
              <dd>{{ currentRelease.min_os || currentVariant.min_os }}</dd>
              <dd class="sf-stat-sub">{{ platformLabel(currentVariant.platform) }}</dd>
            </div>
            <div v-if="packageFormat(currentVariant)" class="sf-stat">
              <dt>{{ t.stats.format }}</dt>
              <dd>{{ packageFormat(currentVariant) }}</dd>
              <dd class="sf-stat-sub">{{ currentVariant.arch || platformLabel(currentVariant.platform) }}</dd>
            </div>
          </dl>
        </div>

        <section class="sf-section">
          <h2>{{ t.sections.whatsNew }}</h2>
          <p class="sf-when" :title="formatFullDate(currentRelease.created_at)">{{ formatTime(currentRelease.created_at) }}</p>
          <p class="sf-build-title">{{ t.variant.version }} {{ versionLabel(currentRelease) }}</p>
          <p v-if="currentRelease.changelog" class="sf-notes">{{ currentRelease.changelog }}</p>
          <p v-else class="sf-notes sf-muted">{{ t.sections.noChangelog }}</p>
        </section>

        <section v-if="otherReleases.length" class="sf-section sf-section-builds">
          <h2>{{ showingLatest ? t.sections.previousBuilds : t.sections.otherBuilds }}</h2>
          <ul class="sf-builds">
            <li v-for="release in otherReleases" :key="release.id" class="sf-build">
              <div class="sf-build-head">
                <div class="sf-build-heading">
                  <p class="sf-build-meta"><span :title="formatFullDate(release.created_at)">{{ formatTime(release.created_at) }}</span><template v-if="release.build"> · {{ t.stats.build }} {{ release.build }}</template></p>
                  <p class="sf-build-title">{{ t.variant.version }} {{ release.version }}</p>
                </div>
                <button type="button" class="sf-pill-button" @click="handleAction(currentVariant, release)">{{ shortActionLabel(currentVariant, release) }}</button>
              </div>
              <p v-if="release.changelog" class="sf-notes sf-notes-small">{{ release.changelog }}</p>
              <p class="sf-build-footer">
                <span v-if="release.channel">{{ release.channel }}</span>
                <span>{{ release.id }}</span>
                <span v-if="formatSize(release.size_bytes)">{{ formatSize(release.size_bytes) }}</span>
              </p>
            </li>
          </ul>
        </section>

        <section class="sf-section">
          <h2>{{ t.sections.information }}</h2>
          <dl class="sf-info">
            <div class="sf-info-row">
              <dt>{{ t.info.platform }}</dt>
              <dd>{{ platformLabel(currentVariant.platform) }}</dd>
            </div>
            <div class="sf-info-row">
              <dt>{{ t.info.identifier }}</dt>
              <dd class="sf-mono">{{ currentVariant.identifier }}</dd>
            </div>
            <div v-if="currentVariant.arch" class="sf-info-row">
              <dt>{{ t.info.architecture }}</dt>
              <dd>{{ currentVariant.arch }}</dd>
            </div>
            <div v-if="currentRelease.channel" class="sf-info-row">
              <dt>{{ t.info.channel }}</dt>
              <dd>{{ currentRelease.channel }}</dd>
            </div>
            <div v-if="currentRelease.download_count" class="sf-info-row">
              <dt>{{ t.info.downloads }}</dt>
              <dd>{{ currentRelease.download_count }}</dd>
            </div>
          </dl>
        </section>
      </template>

      <p v-if="currentVariant?.platform === 'ios'" class="sf-footer-note">{{ t.variant.iosFooter }}</p>
    </main>

    <aside v-if="productData && !loading && !error" class="sf-qr" aria-hidden="true">
      <canvas id="qrCanvas" width="132" height="132"></canvas>
      <span>{{ t.actions.scan }}</span>
    </aside>
  </div>`
}

createApp(location.pathname === '/' ? HomePage : App).mount('#app')

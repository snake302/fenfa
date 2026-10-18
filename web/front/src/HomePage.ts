import { onMounted, ref } from 'vue'
import { useI18n } from './i18n'
import { platformLabel } from './platform'
import { defaultTint, icons } from './chrome'

interface CatalogProduct {
  id: string
  name: string
  description: string
  icon_url: string
  platforms: string[]
  url: string
}

export default {
  setup() {
    const { t } = useI18n()
    const loading = ref(true)
    const error = ref('')
    const products = ref<CatalogProduct[]>([])
    const authEnabled = ref(false)
    const needsLogin = ref(false)
    const password = ref('')
    const submitting = ref(false)
    const loginError = ref('')

    async function initialize() {
      loading.value = true
      error.value = ''
      try {
        const response = await fetch('/api/client/session')
        const payload = await response.json()
        if (!response.ok || !payload.ok) throw new Error('session unavailable')
        authEnabled.value = payload.data.enabled === true
        needsLogin.value = authEnabled.value && !payload.data.authenticated
        if (!needsLogin.value) await loadProducts()
      } catch {
        error.value = t.value.error.loadFailed
      } finally {
        loading.value = false
      }
    }

    async function loadProducts() {
      loading.value = true
      error.value = ''
      try {
        const response = await fetch('/api/products')
        if (response.status === 401) {
          products.value = []
          authEnabled.value = true
          needsLogin.value = true
          return
        }
        const payload = await response.json()
        if (!response.ok || !payload.ok) throw new Error(t.value.error.loadFailed)
        products.value = payload.data.products
      } catch {
        error.value = t.value.error.loadFailed
      } finally {
        loading.value = false
      }
    }

    async function login() {
      if (submitting.value) return
      submitting.value = true
      loginError.value = ''
      try {
        const response = await fetch('/api/client/login', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ password: password.value })
        })
        const payload = await response.json()
        if (!response.ok || !payload.ok) {
          loginError.value = response.status === 401 ? t.value.auth.invalidPassword
            : response.status === 429 ? t.value.auth.rateLimited : t.value.auth.loginFailed
          return
        }
        password.value = ''
        needsLogin.value = false
        await loadProducts()
      } catch {
        loginError.value = t.value.auth.loginFailed
      } finally {
        submitting.value = false
      }
    }

    async function logout() {
      if (submitting.value) return
      submitting.value = true
      try {
        const response = await fetch('/api/client/logout', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: '{}'
        })
        if (!response.ok) throw new Error('logout failed')
        products.value = []
        needsLogin.value = true
        password.value = ''
        loginError.value = ''
        error.value = ''
      } catch {
        error.value = t.value.auth.logoutFailed
      } finally {
        submitting.value = false
      }
    }

    onMounted(initialize)
    function platformList(platforms: string[]) {
      return platforms.map(platformLabel).join(' · ')
    }

    return { t, loading, error, products, platformList, initialize, icons, defaultTint,
      authEnabled, needsLogin, password, submitting, loginError, login, logout }
  },
  template: /* html */ `
    <div class="sf-page">
      <header class="sf-header">
        <div class="sf-header-inner">
          <span class="sf-header-spacer" aria-hidden="true"></span>
          <a href="/" class="sf-wordmark" aria-label="Fenfa"><span>fen</span><span>fa</span></a>
          <button v-if="authEnabled && !needsLogin && !loading" type="button" class="sf-round-button" :disabled="submitting" :aria-label="t.auth.logout" :title="t.auth.logout" @click="logout" v-html="icons.signOut"></button>
          <span v-else class="sf-header-spacer" aria-hidden="true"></span>
        </div>
      </header>

      <main class="sf-main">
        <section class="sf-hero sf-hero-catalog">
          <div class="sf-hero-tint" aria-hidden="true" :style="{ background: defaultTint }"></div>
          <div v-if="needsLogin" class="sf-lock-icon" v-html="icons.lock"></div>
          <h1 class="sf-page-title">{{ needsLogin ? t.auth.title : t.catalog.title }}</h1>
          <p class="sf-page-subtitle">{{ needsLogin ? t.auth.subtitle : t.catalog.subtitle }}</p>
        </section>

        <div v-if="loading" class="sf-state" role="status">
          <div class="sf-spinner"></div>
          <p>{{ t.loading }}</p>
        </div>
        <div v-else-if="error" class="sf-state" role="alert">
          <p>{{ error }}</p>
          <button type="button" class="sf-glass-button sf-glass-button-compact" @click="initialize">{{ t.actions.retry }}</button>
        </div>
        <form v-else-if="needsLogin" class="sf-login" @submit.prevent="login">
          <label for="client-password">{{ t.auth.password }}</label>
          <input id="client-password" v-model="password" type="password" name="password" autocomplete="current-password" required :disabled="submitting" :aria-invalid="!!loginError" :aria-describedby="loginError ? 'client-login-error' : undefined" />
          <p v-if="loginError" id="client-login-error" role="alert" class="sf-login-error">{{ loginError }}</p>
          <button type="submit" class="sf-glass-button" :disabled="submitting || !password">{{ submitting ? t.auth.signingIn : t.auth.signIn }}</button>
        </form>
        <p v-else-if="products.length === 0" class="sf-state">{{ t.catalog.empty }}</p>
        <ul v-else class="sf-catalog" :aria-label="t.catalog.title">
          <li v-for="product in products" :key="product.id">
            <a :href="product.url" class="sf-catalog-row">
              <img v-if="product.icon_url" :src="product.icon_url" alt="" class="sf-catalog-icon" width="60" height="60" />
              <span v-else class="sf-catalog-icon sf-app-icon-fallback" aria-hidden="true">{{ product.name.slice(0, 1).toUpperCase() }}</span>
              <span class="sf-catalog-text">
                <span class="sf-catalog-name">{{ product.name }}</span>
                <span v-if="product.description" class="sf-catalog-description">{{ product.description }}</span>
                <span class="sf-catalog-platforms">{{ platformList(product.platforms) }}</span>
              </span>
              <span class="sf-pill-button" aria-hidden="true">{{ t.actions.view }}</span>
            </a>
          </li>
        </ul>
      </main>
    </div>`
}

import { onMounted, ref } from 'vue'
import { useI18n } from './i18n'
import { platformLabel } from './platform'

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
    return { t, loading, error, products, platformLabel, initialize,
      authEnabled, needsLogin, password, submitting, loginError, login, logout }
  },
  template: /* html */ `
    <div class="page-shell">
      <main class="product-page catalog-page">
        <header class="hero-card catalog-hero">
          <div class="hero-copy">
            <div class="catalog-toolbar">
              <span class="hero-kicker">Fenfa</span>
              <button v-if="authEnabled && !needsLogin && !loading" class="client-logout" :disabled="submitting" @click="logout">{{ t.auth.logout }}</button>
            </div>
            <h1 class="hero-title">{{ needsLogin ? t.auth.title : t.catalog.title }}</h1>
            <p class="hero-subtitle">{{ needsLogin ? t.auth.subtitle : t.catalog.subtitle }}</p>
          </div>
        </header>
        <div v-if="loading" class="catalog-state" role="status">
          <div class="spinner"></div>
          <p>{{ t.loading }}</p>
        </div>
        <div v-else-if="error" class="catalog-state state-error" role="alert">
          <p>{{ error }}</p>
          <button class="primary-action download catalog-retry" @click="initialize">{{ t.actions.retry }}</button>
        </div>
        <form v-else-if="needsLogin" class="client-login" @submit.prevent="login">
          <label for="client-password">{{ t.auth.password }}</label>
          <input id="client-password" v-model="password" type="password" name="password" autocomplete="current-password" required :disabled="submitting" :aria-invalid="!!loginError" :aria-describedby="loginError ? 'client-login-error' : undefined" />
          <p v-if="loginError" id="client-login-error" role="alert" class="client-login-error">{{ loginError }}</p>
          <button type="submit" class="primary-action download" :disabled="submitting || !password">{{ submitting ? t.auth.signingIn : t.auth.signIn }}</button>
        </form>
        <p v-else-if="products.length === 0" class="catalog-state">{{ t.catalog.empty }}</p>
        <section v-else class="catalog-grid" :aria-label="t.catalog.title">
          <a v-for="product in products" :key="product.id" :href="product.url" class="catalog-card">
            <img v-if="product.icon_url" :src="product.icon_url" alt="" class="catalog-icon" />
            <span v-else class="catalog-icon catalog-icon-fallback" aria-hidden="true">{{ product.name.slice(0, 1).toUpperCase() }}</span>
            <h2>{{ product.name }}</h2>
            <p v-if="product.description" class="catalog-description">{{ product.description }}</p>
            <div class="hero-tags">
              <span v-for="platform in product.platforms" :key="platform" class="hero-tag">{{ platformLabel(platform) }}</span>
            </div>
            <span class="catalog-open">{{ t.actions.openProduct }} →</span>
          </a>
        </section>
      </main>
    </div>`
}

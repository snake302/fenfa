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

    async function loadProducts() {
      loading.value = true
      error.value = ''
      try {
        const response = await fetch('/api/products')
        const payload = await response.json()
        if (!response.ok || !payload.ok) throw new Error(t.value.error.loadFailed)
        products.value = payload.data.products
      } catch {
        error.value = t.value.error.loadFailed
      } finally {
        loading.value = false
      }
    }

    onMounted(loadProducts)
    return { t, loading, error, products, platformLabel, loadProducts }
  },
  template: /* html */ `
    <div class="page-shell">
      <main class="product-page catalog-page">
        <header class="hero-card catalog-hero">
          <div class="hero-copy">
            <span class="hero-kicker">Fenfa</span>
            <h1 class="hero-title">{{ t.catalog.title }}</h1>
            <p class="hero-subtitle">{{ t.catalog.subtitle }}</p>
          </div>
        </header>
        <div v-if="loading" class="catalog-state" role="status">
          <div class="spinner"></div>
          <p>{{ t.loading }}</p>
        </div>
        <div v-else-if="error" class="catalog-state state-error" role="alert">
          <p>{{ error }}</p>
          <button class="primary-action download catalog-retry" @click="loadProducts">{{ t.actions.retry }}</button>
        </div>
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

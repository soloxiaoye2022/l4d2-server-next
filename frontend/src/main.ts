import { createApp } from 'vue'
import { createPinia } from 'pinia'
import './style.css'
import App from './App.vue'
import router from './router'
import { usePlatformStore } from './stores/platform'

const app = createApp(App)
const pinia = createPinia()

app.use(pinia)
app.use(router)

// 平台能力在挂载前拉一次，页面渲染时就能拿到正确文案，不会先闪一下 Linux 术语
usePlatformStore(pinia).load().finally(() => {
  app.mount('#app')
})

import { createRouter, createWebHistory } from 'vue-router'
import { CHANNEL_TYPES } from './channelTypes'
import HomePage from './pages/HomePage.vue'
import ChannelListPage from './pages/ChannelListPage.vue'
import ChannelPage from './pages/ChannelPage.vue'
import NotFoundPage from './pages/NotFoundPage.vue'

const BRAND = 'kubemq-ce'

const routes = [
  { path: '/', redirect: '/dashboard' },
  { path: '/dashboard', component: HomePage, meta: { title: 'Home' } },
  ...CHANNEL_TYPES.flatMap((type) => [
    { path: type.path, component: ChannelListPage, meta: { type: type.key, title: type.label } },
    { path: `${type.path}/:name`, component: ChannelPage, props: true, meta: { type: type.key, title: type.label } },
  ]),
  // Paths used by the previous dashboard.
  { path: '/pubsub/:rest(.*)*', redirect: '/events' },
  { path: '/cqrs/:rest(.*)*', redirect: '/commands' },
  { path: '/nodes', redirect: '/dashboard' },
  { path: '/:pathMatch(.*)*', component: NotFoundPage, meta: { title: 'Not found' } },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior: (to, from, saved) => saved ?? (to.path !== from.path ? { top: 0 } : undefined),
})

router.afterEach((to) => {
  const parts = [to.params.name, to.meta.title, BRAND].filter(Boolean)
  document.title = parts.join(' · ')
})

export { BRAND }

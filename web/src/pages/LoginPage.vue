<script setup>
import { ref } from 'vue'
import Icon from '../components/Icon.vue'
import { BRAND } from '../router'
import { login } from '../session'
import { pollSnapshot } from '../snapshot'

const token = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  if (!token.value.trim()) {
    error.value = 'Paste a token.'
    return
  }
  busy.value = true
  error.value = ''
  try {
    await login(token.value)
    token.value = ''
    pollSnapshot()
  } catch (err) {
    error.value = err.message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="login">
    <form class="card panel" @submit.prevent="submit">
      <div class="brand">
        <Icon name="layers" :size="26" />
        <span>{{ BRAND }}</span>
      </div>
      <div>
        <h1>Sign in</h1>
        <p class="muted intro">This broker requires a token. Use the same kind of JWT that clients send.</p>
      </div>
      <label class="field">
        <span>Token</span>
        <input v-model="token" class="input mono" type="password" autocomplete="current-password" spellcheck="false" autofocus placeholder="eyJhbGciOi…" />
      </label>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>
      <button type="submit" class="btn btn-primary" :disabled="busy"><Icon name="lock" />{{ busy ? 'Signing in…' : 'Sign in' }}</button>
      <p class="muted hint">
        Create one on the server with <code>kubemq token --subject &lt;name&gt; --ttl 12h</code>. You stay signed in until the token expires or
        you sign out.
      </p>
    </form>
  </main>
</template>

<style scoped>
.login {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 24px 16px;
}
.panel {
  width: min(100%, 420px);
  display: flex;
  flex-direction: column;
  gap: 18px;
  padding: 28px;
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 1.2rem;
  font-weight: 700;
  color: var(--accent);
}
.brand span {
  color: var(--text-primary);
}
h1 {
  margin: 0 0 4px;
  font-size: 1.35rem;
}
.intro,
.hint {
  margin: 0;
  font-size: 0.875rem;
}
.hint code {
  font-size: 0.8rem;
  overflow-wrap: anywhere;
}
.mono {
  font-family: ui-monospace, "SF Mono", Menlo, Consolas, monospace;
}
</style>

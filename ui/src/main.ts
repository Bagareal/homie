import { mount } from 'svelte'
import '@fontsource/barlow-semi-condensed/400.css'
import '@fontsource/barlow-semi-condensed/500.css'
import '@fontsource/barlow-semi-condensed/600.css'
import '@fontsource/barlow-semi-condensed/700.css'
import './app.css'
import App from './App.svelte'

mount(App, { target: document.getElementById('app')! })

// Makes the app installable. Only in the built app (so `make dev` never gets
// stuck behind a cached worker), and browsers only allow it over HTTPS.
if (import.meta.env.PROD && 'serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('sw.js').catch(() => {})
  })
}

import { mount } from 'svelte'
import '@fontsource/barlow-semi-condensed/400.css'
import '@fontsource/barlow-semi-condensed/500.css'
import '@fontsource/barlow-semi-condensed/600.css'
import '@fontsource/barlow-semi-condensed/700.css'
import './app.css'
import App from './App.svelte'

mount(App, { target: document.getElementById('app')! })

<script lang="ts">
  import { joinAddress, splitAddress } from '../lib/address'

  // The address and its port side by side; the port's placeholder is the default one,
  // used when the field is left empty.
  let {
    value = $bindable(''),
    placeholder,
    port,
    label = 'Address',
  }: { value: string; placeholder: string; port: string; label?: string } = $props()

  const parts = $derived(splitAddress(value))
</script>

<div class="addr">
  <input
    value={parts.host}
    {placeholder}
    inputmode="url"
    aria-label={label}
    oninput={(e) => (value = joinAddress(e.currentTarget.value, parts.port))}
  />
  <input
    class="port"
    value={parts.port}
    placeholder={port}
    inputmode="numeric"
    maxlength="5"
    aria-label="Port"
    title="Port: leave empty for the default ({port})"
    oninput={(e) => (value = joinAddress(parts.host, e.currentTarget.value.replace(/\D/g, '')))}
  />
</div>

<style>
  .addr {
    display: grid;
    grid-template-columns: 1fr 96px;
    gap: 10px;
  }
  .port {
    text-align: center;
  }
</style>

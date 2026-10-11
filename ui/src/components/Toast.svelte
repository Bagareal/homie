<script lang="ts">
  import { app } from '../lib/state.svelte'
</script>

<div class="toast-area" role="status" aria-live="polite">
  {#if app.toast}
    {#key app.toast.key}
      <p class="toast" class:error={app.toast.error}>{app.toast.text}</p>
    {/key}
  {/if}
</div>

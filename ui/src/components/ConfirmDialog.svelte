<script lang="ts">
  import { onMount } from 'svelte'
  import { app, type Ask } from '../lib/state.svelte'

  let { ask }: { ask: Ask } = $props()

  let dialog: HTMLDialogElement

  onMount(() => {
    dialog.showModal()
    dialog.querySelector<HTMLElement>('[data-focus]')?.focus()
  })
</script>

<dialog
  bind:this={dialog}
  class="dialog confirm"
  aria-labelledby="confirm-title"
  oncancel={(e) => {
    e.preventDefault()
    app.answer(null)
  }}
>
  <div class="confirm-body">
    <h2 id="confirm-title">{ask.title}</h2>
    <p>{ask.message}</p>
    <div class="dialog-actions">
      {#each ask.buttons as b (b.value)}
        <button
          type="button"
          class:primary={b.kind === 'primary'}
          class:danger={b.kind === 'danger'}
          data-focus={b.focus ? '' : undefined}
          onclick={() => app.answer(b.value)}>{b.label}</button
        >
      {/each}
    </div>
  </div>
</dialog>

<script lang="ts">
  import { onMount } from 'svelte'
  import { app, type Ask } from '../lib/state.svelte'

  let { ask }: { ask: Ask } = $props()

  let dialog: HTMLDialogElement
  // svelte-ignore state_referenced_locally
  let text = $state(ask.input?.value ?? '')
  let field: HTMLInputElement | undefined = $state()

  onMount(() => {
    dialog.showModal()
    if (field) field.select()
    else dialog.querySelector<HTMLElement>('[data-focus]')?.focus()
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
    <form
      onsubmit={(e) => {
        e.preventDefault()
        // Enter in the text field presses the primary button.
        const primary = ask.buttons.find((b) => b.kind === 'primary')
        if (primary) app.answer(primary.value, text)
      }}
    >
      {#if ask.input}
        <input bind:this={field} bind:value={text} placeholder={ask.input.placeholder} maxlength="60" aria-label={ask.title} />
      {/if}
      <div class="dialog-actions">
        {#each ask.buttons as b (b.value)}
          <button
            type={b.kind === 'primary' ? 'submit' : 'button'}
            class:primary={b.kind === 'primary'}
            class:danger={b.kind === 'danger'}
            data-focus={b.focus ? '' : undefined}
            onclick={b.kind === 'primary' ? undefined : () => app.answer(b.value, text)}>{b.label}</button
          >
        {/each}
      </div>
    </form>
  </div>
</dialog>

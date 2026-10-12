<script lang="ts">
  import { onMount } from 'svelte'
  import { app } from '../lib/state.svelte'
  import { clearDraft, saveDraft } from '../lib/draft'

  let { groupId, draft = null, onclose }: { groupId: string | null; draft?: string | null; onclose: () => void } = $props()

  let dialog: HTMLDialogElement
  let form: HTMLFormElement
  // svelte-ignore state_referenced_locally
  const group = groupId ? app.group(groupId) : undefined
  // svelte-ignore state_referenced_locally
  let name = $state(draft ?? group?.name ?? '')
  let error = $state('')

  const dirty = $derived(name !== (group?.name ?? ''))

  // Keep a copy of unsaved edits so a refresh can offer to restore them.
  $effect(() => {
    if (dirty) saveDraft({ kind: 'group', groupId: group?.id ?? null, name })
    else clearDraft()
  })

  async function requestClose() {
    if (!dirty) return dialog.close()
    const choice = await app.confirmUnsaved('group')
    if (choice === 'discard') dialog.close()
    else if (choice === 'save') form.requestSubmit()
  }

  onMount(() => dialog.showModal())

  function submit(e: SubmitEvent) {
    e.preventDefault()
    if (!name.trim()) return void (error = 'Enter a name for the group.')
    app.upsertGroup(group?.id ?? null, name.trim())
    dialog.close()
  }

  async function remove() {
    if (!group) return
    const n = group.items.length
    const message =
      n === 0
        ? `Delete the group “${group.name}”?`
        : `The group “${group.name}” contains ${n === 1 ? '1 service' : `${n} services`}, which will be deleted. Continue?`
    const ok = await app.confirm({ title: 'Delete group', message, confirmLabel: 'Delete', danger: true })
    if (ok) {
      app.deleteGroup(group.id)
      dialog.close()
    }
  }
</script>

<svelte:window
  onbeforeunload={(e) => {
    if (dirty) e.preventDefault()
  }}
/>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<dialog
  bind:this={dialog}
  class="dialog"
  onclose={() => {
    clearDraft()
    onclose()
  }}
  oncancel={(e) => {
    e.preventDefault()
    requestClose()
  }}
  onclick={(e) => e.target === dialog && requestClose()}
>
  <form bind:this={form} onsubmit={submit} novalidate>
    <h2>{group ? 'Edit group' : 'New group'}</h2>
    <label>
      Name
      <!-- svelte-ignore a11y_autofocus -->
      <input bind:value={name} maxlength="60" placeholder="Infrastructure" autofocus />
    </label>

    {#if error}<p class="form-error" role="alert">{error}</p>{/if}
    <div class="dialog-actions">
      {#if group}<button type="button" class="danger" onclick={remove}>Delete group</button>{/if}
      <button type="button" onclick={requestClose}>Cancel</button>
      <button type="submit" class="primary">{group ? 'Save changes' : 'Add group'}</button>
    </div>
  </form>
</dialog>

<script lang="ts">
  import { onMount } from 'svelte'
  import { app } from '../lib/state.svelte'

  let { groupId, onclose }: { groupId: string | null; onclose: () => void } = $props()

  let dialog: HTMLDialogElement
  // svelte-ignore state_referenced_locally
  const group = groupId ? app.group(groupId) : undefined
  let name = $state(group?.name ?? '')
  let error = $state('')

  onMount(() => dialog.showModal())

  function submit(e: SubmitEvent) {
    e.preventDefault()
    if (!name.trim()) return void (error = 'Enter a name for the group.')
    app.upsertGroup(group?.id ?? null, name.trim())
    dialog.close()
  }

  function remove() {
    if (!group) return
    const n = group.items.length
    const msg =
      n === 0
        ? `Delete the group “${group.name}”?`
        : `The group “${group.name}” contains ${n === 1 ? '1 service' : `${n} services`}, which will be deleted. Continue?`
    if (confirm(msg)) {
      app.deleteGroup(group.id)
      dialog.close()
    }
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<dialog bind:this={dialog} class="dialog" {onclose} onclick={(e) => e.target === dialog && dialog.close()}>
  <form onsubmit={submit} novalidate>
    <h2>{group ? 'Edit group' : 'New group'}</h2>
    <label>
      Name
      <!-- svelte-ignore a11y_autofocus -->
      <input bind:value={name} maxlength="60" placeholder="Infrastructure" autofocus />
    </label>

    {#if error}<p class="form-error" role="alert">{error}</p>{/if}
    <div class="dialog-actions">
      {#if group}<button type="button" class="danger" onclick={remove}>Delete group</button>{/if}
      <button type="button" onclick={() => dialog.close()}>Cancel</button>
      <button type="submit" class="primary">{group ? 'Save changes' : 'Add group'}</button>
    </div>
  </form>
</dialog>

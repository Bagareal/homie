<script lang="ts">
  import { onMount } from 'svelte'
  import { app, uid } from '../lib/state.svelte'
  import type { Item } from '../lib/types'
  import Icon from './Icon.svelte'

  let { groupId, item, onclose }: { groupId: string; item: Item | null; onclose: () => void } = $props()

  let dialog: HTMLDialogElement
  // Fields start from the service's values and are edited locally until saved.
  // svelte-ignore state_referenced_locally
  let name = $state(item?.name ?? '')
  // svelte-ignore state_referenced_locally
  let url = $state(item?.url ?? '')
  // svelte-ignore state_referenced_locally
  let icon = $state(item?.icon ?? '')
  // svelte-ignore state_referenced_locally
  let description = $state(item?.description ?? '')
  // svelte-ignore state_referenced_locally
  let check = $state(item?.check ?? true)
  // svelte-ignore state_referenced_locally
  let iconInset = $state(item?.iconInset ?? false)
  // svelte-ignore state_referenced_locally
  let target = $state(groupId)
  let error = $state('')

  onMount(() => dialog.showModal())

  function normalizeUrl(raw: string): string {
    const v = raw.trim()
    return v && !/^[a-z][a-z0-9+.-]*:\/\//i.test(v) ? `http://${v}` : v
  }

  function submit(e: SubmitEvent) {
    e.preventDefault()
    const finalUrl = normalizeUrl(url)
    if (!name.trim()) return void (error = 'Enter a name for the service.')
    try {
      const u = new URL(finalUrl)
      if (u.protocol !== 'http:' && u.protocol !== 'https:') throw new Error()
    } catch {
      return void (error = 'Enter a valid address, for example https://192.168.1.10:8006.')
    }
    app.upsertItem(groupId, target, {
      id: item?.id ?? uid(),
      name: name.trim(),
      url: finalUrl,
      icon: icon.trim(),
      description: description.trim(),
      check,
      iconInset,
    })
    dialog.close()
  }

  function remove() {
    if (item && confirm(`Remove “${item.name}” from the dashboard?`)) {
      app.deleteItem(groupId, item.id)
      dialog.close()
    }
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<dialog bind:this={dialog} class="dialog" {onclose} onclick={(e) => e.target === dialog && dialog.close()}>
  <form onsubmit={submit} novalidate>
    <h2>{item ? 'Edit service' : 'New service'}</h2>

    <label>
      Name
      <!-- svelte-ignore a11y_autofocus -->
      <input bind:value={name} maxlength="80" autofocus />
    </label>

    <label>
      Address
      <input bind:value={url} placeholder="https://192.168.1.10:8006" inputmode="url" />
    </label>

    <label>
      Icon
      <span class="with-preview">
        <input bind:value={icon} placeholder="proxmox" maxlength="500" />
        <Icon {icon} name={name || '?'} inset={iconInset} />
      </span>
      <small>Name from the dashboard-icons collection (e.g. proxmox, truenas-scale, adguard-home), an image address, a file in /icons/, or an emoji.</small>
    </label>

    <label class="check">
      <input type="checkbox" bind:checked={iconInset} />
      Shrink icon (for logos that fill their whole square, to match the others)
    </label>

    <label>
      Description
      <input bind:value={description} maxlength="200" placeholder="If empty, shows the address" />
    </label>

    <label>
      Group
      <select bind:value={target}>
        {#each app.config!.groups as g (g.id)}
          <option value={g.id}>{g.name}</option>
        {/each}
      </select>
    </label>

    <label class="check">
      <input type="checkbox" bind:checked={check} />
      Check whether the service is online
    </label>

    {#if error}<p class="form-error" role="alert">{error}</p>{/if}

    <div class="dialog-actions">
      {#if item}<button type="button" class="danger" onclick={remove}>Delete</button>{/if}
      <button type="button" onclick={() => dialog.close()}>Cancel</button>
      <button type="submit" class="primary">{item ? 'Save changes' : 'Add service'}</button>
    </div>
  </form>
</dialog>

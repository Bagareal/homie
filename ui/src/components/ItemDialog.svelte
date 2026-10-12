<script lang="ts">
  import { onMount } from 'svelte'
  import { app, uid } from '../lib/state.svelte'
  import { clearDraft, saveDraft, type ItemFields } from '../lib/draft'
  import type { Item } from '../lib/types'
  import Icon from './Icon.svelte'

  let {
    groupId,
    item,
    draft = null,
    onclose,
  }: { groupId: string; item: Item | null; draft?: ItemFields | null; onclose: () => void } = $props()

  let dialog: HTMLDialogElement
  let form: HTMLFormElement
  // Fields start from the service's values (or a restored draft) and are
  // edited locally until saved.
  // svelte-ignore state_referenced_locally
  const initial: ItemFields = {
    name: item?.name ?? '',
    url: item?.url ?? '',
    icon: item?.icon ?? '',
    description: item?.description ?? '',
    check: item?.check ?? true,
    target: groupId,
  }
  // svelte-ignore state_referenced_locally
  const start = draft ?? initial
  let name = $state(start.name)
  let url = $state(start.url)
  let icon = $state(start.icon)
  let description = $state(start.description)
  let check = $state(start.check)
  let target = $state(start.target)
  let error = $state('')

  const dirty = $derived(
    name !== initial.name ||
      url !== initial.url ||
      icon !== initial.icon ||
      description !== initial.description ||
      check !== initial.check ||
      target !== initial.target,
  )

  // Keep a copy of unsaved edits so a refresh can offer to restore them.
  $effect(() => {
    if (dirty) saveDraft({ kind: 'item', groupId, itemId: item?.id ?? null, fields: { name, url, icon, description, check, target } })
    else clearDraft()
  })

  async function requestClose() {
    if (!dirty) return dialog.close()
    const choice = await app.confirmUnsaved('service')
    if (choice === 'discard') dialog.close()
    else if (choice === 'save') form.requestSubmit()
  }

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
    })
    dialog.close()
  }

  async function remove() {
    if (!item) return
    const ok = await app.confirm({
      title: 'Remove service',
      message: `Remove “${item.name}” from the dashboard?`,
      confirmLabel: 'Remove',
      danger: true,
    })
    if (ok) {
      app.deleteItem(groupId, item.id)
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
        <Icon {icon} name={name || '?'} />
      </span>
      <small>Name from the dashboard-icons collection (e.g. proxmox, truenas-scale, adguard-home), an image address, a file in /icons/, or an emoji.</small>
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
      <button type="button" onclick={requestClose}>Cancel</button>
      <button type="submit" class="primary">{item ? 'Save changes' : 'Add service'}</button>
    </div>
  </form>
</dialog>

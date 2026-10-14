import { app } from './state.svelte'
import { mobile } from './mobile.svelte'

export type WidgetLayout = 'masonry' | 'column' | 'strip'

// Where the widgets go next to the groups (Settings → Appearance → Widgets). A
// phone has its own tabs for them, so there they just stack like the groups.
export const arrange = {
  get mode(): WidgetLayout {
    const m = app.config?.settings.widgetLayout
    return mobile.narrow ? 'masonry' : m === 'column' || m === 'strip' ? m : 'masonry'
  },
  get right(): boolean {
    return app.config?.settings.widgetSide === 'right'
  },
  // Column and strip keep widgets and groups in zones of their own.
  get separate(): boolean {
    return this.mode !== 'masonry'
  },
  // Which way a block's neighbours lie, for the drop markers while dragging.
  get groupsVertical(): boolean {
    return this.mode === 'masonry'
  },
  get widgetsVertical(): boolean {
    return this.mode !== 'strip'
  },
}

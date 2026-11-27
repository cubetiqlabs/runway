import { createStore, useSelector } from "@tanstack/react-store"

export interface AppStoreState {
  csrfToken: string
  livePolling: boolean
  mobileNavOpen: boolean
  tableColumnVisibility: Record<string, Record<string, boolean>>
  tablePageSizes: Record<string, number>
}

const STORAGE_KEY = "runway:ui-store:v1"

interface PersistedSlice {
  livePolling?: boolean
  tableColumnVisibility?: Record<string, Record<string, boolean>>
  tablePageSizes?: Record<string, number>
}

function loadPersistedState(): PersistedSlice {
  if (typeof window === "undefined") {
    return {}
  }
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    if (!raw) return {}
    return JSON.parse(raw) as PersistedSlice
  } catch {
    return {}
  }
}

function persistState(state: AppStoreState): void {
  if (typeof window === "undefined") {
    return
  }
  try {
    const payload: PersistedSlice = {
      livePolling: state.livePolling,
      tableColumnVisibility: state.tableColumnVisibility,
      tablePageSizes: state.tablePageSizes,
    }
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(payload))
  } catch {
    // Ignore storage quota / privacy mode errors
  }
}

const initialPersisted = loadPersistedState()

export const appStore = createStore<AppStoreState>({
  csrfToken: "",
  livePolling: initialPersisted.livePolling ?? true,
  mobileNavOpen: false,
  tableColumnVisibility: initialPersisted.tableColumnVisibility ?? {},
  tablePageSizes: initialPersisted.tablePageSizes ?? {},
})

export function useAppSelector<T>(selector: (state: AppStoreState) => T): T {
  return useSelector(appStore, selector)
}

export function setCsrfTokenState(token: string): void {
  appStore.setState((prev) => (prev.csrfToken === token ? prev : { ...prev, csrfToken: token }))
}

export function toggleLivePolling(): void {
  appStore.setState((prev) => {
    const next = { ...prev, livePolling: !prev.livePolling }
    persistState(next)
    return next
  })
}

export function setMobileNavOpen(open: boolean): void {
  appStore.setState((prev) => (prev.mobileNavOpen === open ? prev : { ...prev, mobileNavOpen: open }))
}

export function setTableColumnVisibility(tableId: string, visibility: Record<string, boolean>): void {
  appStore.setState((prev) => {
    const next: AppStoreState = {
      ...prev,
      tableColumnVisibility: {
        ...prev.tableColumnVisibility,
        [tableId]: visibility,
      },
    }
    persistState(next)
    return next
  })
}

export function clearTableColumnVisibility(tableId: string): void {
  appStore.setState((prev) => {
    const updated = { ...prev.tableColumnVisibility }
    delete updated[tableId]
    const next: AppStoreState = { ...prev, tableColumnVisibility: updated }
    persistState(next)
    return next
  })
}

export function setTablePageSize(tableId: string, pageSize: number): void {
  appStore.setState((prev) => {
    const next: AppStoreState = {
      ...prev,
      tablePageSizes: {
        ...prev.tablePageSizes,
        [tableId]: pageSize,
      },
    }
    persistState(next)
    return next
  })
}

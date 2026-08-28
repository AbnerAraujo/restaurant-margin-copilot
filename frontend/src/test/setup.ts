import '@testing-library/jest-dom/vitest'

// jsdom implements no layout engine and therefore no scrolling API:
// `Element.prototype.scrollTo` simply does not exist, so any component that
// drives its own scroll position (ChatPanel pinning the log to the newest
// message) throws on mount under test. Stubbed here rather than defended
// against in product code — a component should not carry a branch that
// exists only because the test environment can't lay anything out.
if (typeof Element.prototype.scrollTo !== 'function') {
  Element.prototype.scrollTo = function scrollTo(
    optionsOrX?: ScrollToOptions | number,
    y?: number,
  ) {
    const top =
      typeof optionsOrX === 'object' ? optionsOrX?.top : y
    if (typeof top === 'number') this.scrollTop = top
  }
}

// This environment provides `window` but no `localStorage`: Node 26 ships its
// own experimental implementation that is inert without `--localstorage-file`,
// and it shadows the one jsdom would otherwise install. Anything exercising
// client-side persistence would therefore see storage as permanently
// unavailable — which the product code survives by design, but which makes it
// impossible to TEST that persistence actually works. An in-memory Storage
// stands in, with the real API surface so `Storage.prototype` can still be
// spied on for the quota-exceeded case.
if (typeof globalThis.localStorage === 'undefined') {
  const store = new Map<string, string>()
  class MemoryStorage implements Storage {
    get length() {
      return store.size
    }
    clear() {
      store.clear()
    }
    getItem(key: string) {
      return store.has(key) ? (store.get(key) as string) : null
    }
    key(index: number) {
      return [...store.keys()][index] ?? null
    }
    removeItem(key: string) {
      store.delete(key)
    }
    setItem(key: string, value: string) {
      store.set(key, String(value))
    }
  }
  const storage = new MemoryStorage()
  globalThis.Storage = MemoryStorage as unknown as typeof Storage
  Object.defineProperty(globalThis, 'localStorage', { value: storage, configurable: true })
  Object.defineProperty(window, 'localStorage', { value: storage, configurable: true })
}

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

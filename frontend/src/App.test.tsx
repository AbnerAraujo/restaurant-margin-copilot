import { render, screen } from '@testing-library/react'
import { beforeAll, describe, expect, it } from 'vitest'
import App from './App'

// jsdom has no ResizeObserver; Radix's ScrollArea (inside the chat panel
// App renders) needs one to mount.
beforeAll(() => {
  if (!('ResizeObserver' in globalThis)) {
    class ResizeObserverStub {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
    globalThis.ResizeObserver = ResizeObserverStub
  }
})

describe('App', () => {
  it("renders the Margin Copilot page — today's close summary and the Q&A chat panel", () => {
    render(<App />)
    expect(
      screen.getByRole('heading', { name: /today's close/i }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('heading', { name: /ask about your margin/i }),
    ).toBeInTheDocument()
  })
})

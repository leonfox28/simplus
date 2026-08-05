import React from 'react'
import { describe, expect, it, vi } from 'vitest'
import { layout } from './app'

function clickMenuItem(isMobile: boolean) {
  const collapse = vi.fn()
  const rendered = layout({ initialState: { setupRequired: false } }).menuItemRender(
    { path: '/mihomo', isMobile, onClick: collapse },
    React.createElement('span', null, 'Mihomo'),
  ) as React.ReactElement<{ onClick: (event: { preventDefault: () => void }) => void }>
  rendered.props.onClick({ preventDefault: vi.fn() })
  return collapse
}

describe('layout menu behavior', () => {
  it('keeps the desktop sidebar expanded after navigation', () => {
    expect(clickMenuItem(false)).not.toHaveBeenCalled()
  })

  it('closes the mobile drawer after navigation', () => {
    expect(clickMenuItem(true)).toHaveBeenCalledOnce()
  })
})

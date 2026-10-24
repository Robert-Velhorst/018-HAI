describe('Shared global styles', () => {
  let host: HTMLDivElement
  let wasDark: boolean

  beforeEach(() => {
    wasDark = document.body.classList.contains('hai-theme-dark')
    host = document.createElement('div')
    host.style.cssText = 'position:fixed;left:20px;top:20px;width:240px;height:160px;z-index:100000'
    document.body.appendChild(host)
  })

  afterEach(() => {
    host.remove()
    document.body.classList.toggle('hai-theme-dark', wasDark)
  })

  function notification(nested: boolean): HTMLButtonElement {
    host.className = 'cdk-overlay-container'
    host.innerHTML = `<button id="underlying" style="position:absolute;inset:0;width:240px;height:160px">Continue</button>
      <div class="cdk-overlay-pane" style="position:absolute;inset:0">
        ${nested ? '<div>' : ''}<nz-notification-container>
          <div class="ant-notification"><div class="ant-notification-notice" style="position:absolute;inset:0">
            <span style="position:absolute;inset:0">Saved</span>
            <button class="ant-notification-notice-close" style="position:absolute;right:0;top:0;width:32px;height:32px">Close</button>
          </div></div>
        </nz-notification-container>${nested ? '</div>' : ''}
      </div>`
    return host.querySelector('#underlying') as HTMLButtonElement
  }

  for (const nested of [false, true]) {
    it(`keeps ${nested ? 'nested' : 'direct'} notification panes out of underlying action hit-testing`, () => {
      const action = notification(nested)
      expect(document.elementFromPoint(100, 100)).toBe(action)
      expect(getComputedStyle(host.querySelector('.cdk-overlay-pane')!).pointerEvents).toBe('none')
    })
  }

  it('keeps notification dismissal clickable without making its text intercept actions', () => {
    notification(true)
    const close = host.querySelector('.ant-notification-notice-close') as HTMLButtonElement
    expect(document.elementFromPoint(244, 36)).toBe(close)
    const dismissed = jasmine.createSpy('dismissed')
    close.addEventListener('click', dismissed)
    close.click()
    expect(dismissed).toHaveBeenCalledTimes(1)
    expect(getComputedStyle(host.querySelector('.ant-notification-notice span')!).pointerEvents).toBe('none')
  })

  it('does not disable an unrelated interactive overlay pane', () => {
    host.className = 'cdk-overlay-container'
    host.innerHTML = '<div class="cdk-overlay-pane" style="position:absolute;inset:0"><button style="width:240px;height:160px">Approve</button></div>'
    expect(getComputedStyle(host.firstElementChild!).pointerEvents).toBe('auto')
    expect(document.elementFromPoint(100, 100)).toBe(host.querySelector('button'))
  })

  it('provides distinct light and dark surface and text tokens', () => {
    document.body.classList.remove('hai-theme-dark')
    const light = getComputedStyle(document.body)
    const lightText = light.getPropertyValue('--hai-text').trim()
    const lightSurface = light.getPropertyValue('--hai-surface').trim()
    document.body.classList.add('hai-theme-dark')
    const dark = getComputedStyle(document.body)
    expect(dark.getPropertyValue('--hai-text').trim()).not.toBe(lightText)
    expect(dark.getPropertyValue('--hai-surface').trim()).not.toBe(lightSurface)
    expect(lightText).toBe('#142033')
    expect(lightSurface).toBe('#ffffff')
  })

  it('uses dark text tokens for every native heading level', () => {
    document.body.classList.add('hai-theme-dark')
    host.innerHTML = '<h1>One</h1><h2>Two</h2><h3>Three</h3><h4>Four</h4><h5>Five</h5><h6>Six</h6>'
    for (const heading of Array.from(host.children)) {
      expect(getComputedStyle(heading).color).toBe('rgb(237, 244, 255)')
    }
  })

  it('keeps diagnostics modal surfaces on the active theme', () => {
    document.body.classList.add('hai-theme-dark')
    host.className = 'hai-execution-diagnostics'
    host.innerHTML = '<div class="ant-modal-content"><div class="ant-modal-header"><div class="ant-modal-title">Details</div></div><div class="ant-modal-body">Evidence</div></div>'
    expect(getComputedStyle(host.querySelector('.ant-modal-content')!).backgroundColor).toBe('rgb(17, 29, 46)')
    expect(getComputedStyle(host.querySelector('.ant-modal-title')!).color).toBe('rgb(237, 244, 255)')
    expect(getComputedStyle(host.querySelector('.ant-modal-body')!).color).toBe('rgb(237, 244, 255)')
  })
})

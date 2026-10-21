(() => {
  let trigger;
  let overlay;
  const scrim = document.getElementById('scrim');
  const drawer = document.getElementById('drawer');
  const modal = document.getElementById('action-modal');
  const focusable = 'a[href], button:not([disabled]), input:not([disabled]):not([type=hidden]), select:not([disabled]), textarea:not([disabled]), summary, [tabindex="0"]';

  function closeOverlay() {
    if (!overlay) return;
    overlay.classList.remove('open');
    overlay.hidden = true;
    overlay.inert = true;
    scrim.classList.remove('open');
    scrim.hidden = true;
    document.body.classList.remove('overlay-open');
    overlay = null;
    trigger?.focus();
  }

  function openOverlay(element) {
    if (!element) return;
    if (overlay && overlay !== element) { overlay.classList.remove('open'); overlay.hidden = true; overlay.inert = true; }
    overlay = element;
    element.hidden = false;
    element.inert = false;
    scrim.hidden = false;
    element.classList.add('open');
    scrim.classList.add('open');
    document.body.classList.add('overlay-open');
    (element.querySelector(focusable) || element).focus();
  }

  document.addEventListener('click', event => {
    const link = event.target.closest('[data-drawer], [data-modal]');
    if (link && !event.ctrlKey && !event.metaKey && !event.shiftKey && !event.altKey && event.button === 0) trigger = link;
    if (event.target.closest('[data-close-overlay]') && overlay) { event.preventDefault(); closeOverlay(); }
    document.querySelectorAll('.account-menu[open], .action-menu[open]').forEach(menu => {
      if (!menu.contains(event.target)) menu.open = false;
    });
  });

  document.addEventListener('htmx:afterSwap', event => {
    if (event.detail.target.id === 'drawer-body') openOverlay(drawer);
    if (event.detail.target.id === 'modal-content') openOverlay(modal);
  });
  // Validation responses replace the submitted form inside its current overlay.
  document.addEventListener('htmx:beforeSwap', event => {
    const target = event.detail.target.id;
    if ((target === 'drawer-body' || target === 'modal-content') && [400, 409, 422].includes(event.detail.xhr.status)) {
      event.detail.shouldSwap = true;
      event.detail.isError = false;
    }
  });
  // Failed loads follow the original URL; POSTs are never automatically replayed.
  document.addEventListener('htmx:responseError', event => {
    if (['drawer-body', 'modal-content'].includes(event.detail.target.id) && trigger?.href && event.detail.requestConfig.verb === 'get') window.location.assign(trigger.href);
  });
  document.addEventListener('htmx:sendError', event => {
    if (['drawer-body', 'modal-content'].includes(event.detail.target.id) && trigger?.href && event.detail.requestConfig.verb === 'get') window.location.assign(trigger.href);
  });

  document.addEventListener('keydown', event => {
    if (event.key === 'Escape') {
      closeOverlay();
      document.querySelectorAll('.account-menu[open], .action-menu[open]').forEach(menu => {
        menu.open = false;
        menu.querySelector('summary').focus();
      });
    }
    if (event.key === 'Tab' && overlay) {
      const nodes = [...overlay.querySelectorAll(focusable)].filter(node => node.getClientRects().length);
      const first = nodes[0], last = nodes[nodes.length - 1];
      if (!first) { event.preventDefault(); overlay.focus(); }
      else if (event.shiftKey && (document.activeElement === first || document.activeElement === overlay)) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    }
  });

  const today = document.getElementById('today-col');
  if (today) {
    const container = today.closest('.cal-wrap');
    const cell = today.getBoundingClientRect();
    const unitWidth = container.querySelector('.unit-cell').getBoundingClientRect().width;
    container.scrollLeft += cell.left - container.getBoundingClientRect().left - unitWidth - (container.clientWidth - unitWidth - cell.width) / 2;
  }
})();

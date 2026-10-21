(() => {
  let trigger;
  let overlay;
  const scrim = document.getElementById('scrim');
  const drawer = document.getElementById('drawer');
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
    const link = event.target.closest('[data-drawer]');
    if (link && !event.ctrlKey && !event.metaKey && !event.shiftKey && !event.altKey && event.button === 0) trigger = link;
    if (event.target.closest('[data-close-overlay]')) closeOverlay();
    const modalButton = event.target.closest('[data-modal]');
    if (modalButton) {
      trigger = modalButton;
      openOverlay(document.getElementById(modalButton.dataset.modal));
    }
    document.querySelectorAll('.account-menu[open]').forEach(menu => {
      if (!menu.contains(event.target)) menu.open = false;
    });
  });

  document.addEventListener('htmx:afterSwap', event => {
    if (event.detail.target.id === 'drawer-body') openOverlay(drawer);
  });
  // A failed enhanced request follows its ordinary link so the full error remains visible.
  document.addEventListener('htmx:responseError', event => {
    if (event.detail.target.id === 'drawer-body' && trigger) window.location.assign(trigger.href);
  });
  document.addEventListener('htmx:sendError', event => {
    if (event.detail.target.id === 'drawer-body' && trigger) window.location.assign(trigger.href);
  });

  document.addEventListener('keydown', event => {
    if (event.key === 'Escape') {
      closeOverlay();
      document.querySelectorAll('.account-menu[open]').forEach(menu => {
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

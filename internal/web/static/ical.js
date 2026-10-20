document.addEventListener('submit', (event) => {
  const message = event.target.dataset.confirm;
  if (message && !window.confirm(message)) event.preventDefault();
});
document.addEventListener('click', async (event) => {
  const button = event.target.closest('[data-copy-target]');
  if (!button) return;
  try {
    await navigator.clipboard.writeText(document.getElementById(button.dataset.copyTarget).value);
    button.textContent = button.dataset.copied;
  } catch (error) {
    button.textContent = button.dataset.copyError;
    console.error('Copy calendar URL:', error);
  }
});

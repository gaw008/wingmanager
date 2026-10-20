{
  const today = document.querySelector('.calendar-grid thead [aria-current="date"]');
  if (today) {
    const container = today.closest('.overflow-auto');
    const cell = today.getBoundingClientRect();
    // Scroll only the calendar; scrollIntoView would also move the page vertically.
    container.scrollLeft += cell.left - container.getBoundingClientRect().left - (container.clientWidth - cell.width) / 2;
  }
}

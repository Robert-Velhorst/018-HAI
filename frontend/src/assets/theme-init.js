(function () {
  var mode = 'dark';
  try {
    mode = localStorage.getItem('hai-theme-mode') || localStorage.getItem('hai-control-center-theme') || 'dark';
  } catch (_) {}
  mode = mode === 'light' ? 'light' : 'dark';
  var themeClass = 'hai-theme-' + mode;
  document.documentElement.classList.add(themeClass);
  document.documentElement.setAttribute('data-hai-theme', mode);
  document.documentElement.style.colorScheme = mode;
}());

var themeToggle = document.getElementById('theme-toggle');
themeToggle.setAttribute('aria-pressed', document.documentElement.classList.contains('dark'));
themeToggle.addEventListener('click', function () {
  var isDark = document.documentElement.classList.toggle('dark');
  themeToggle.setAttribute('aria-pressed', isDark);
  try { localStorage.setItem('theme', isDark ? 'dark' : 'light'); } catch (e) {}
});

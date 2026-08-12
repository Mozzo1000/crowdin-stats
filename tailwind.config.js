/** @type {import('tailwindcss').Config} */
module.exports = {
  darkMode: "class",
  content: ["./static/*.html", "./static/*.js"],
  theme: {
    extend: {
      colors: {
        // Semantic tokens backed by CSS variables (see input.css) so a
        // single `.dark` class on <html> re-themes every component —
        // no dark: variant needed on individual elements.
        bg: "var(--bg)",
        "bg-subtle": "var(--bg-subtle)",
        surface: "var(--surface)",
        text: "var(--text)",
        "text-muted": "var(--text-muted)",
        border: "var(--border)",
        accent: "var(--accent)",
        "accent-hover": "var(--accent-hover)",
        "accent-contrast": "var(--accent-contrast)",
        "accent-soft": "var(--accent-soft)",
        success: "var(--success)",
        "success-soft": "var(--success-soft)",
      },
      fontFamily: {
        sans: ["Inter", "Helvetica", "Arial", "sans-serif"],
        mono: ["JetBrains Mono", "ui-monospace", "monospace"],
      },
    },
  },
  plugins: [],
};

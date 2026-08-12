/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./static/*.html"],
  theme: {
    extend: {
      colors: {
        ink: "#0B0E14",
        surface: "#12161F",
        text: "#E8EAED",
        "text-muted": "#8B93A3",
        border: "#232834",
        "accent-mint": "#7DD3A8",
        "accent-amber": "#F5A623",
      },
      fontFamily: {
        sans: ["Inter", "Helvetica", "Arial", "sans-serif"],
        mono: ["JetBrains Mono", "ui-monospace", "monospace"],
      },
    },
  },
  plugins: [],
};

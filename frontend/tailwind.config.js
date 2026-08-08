/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    './app/**/*.{js,ts,jsx,tsx,mdx}',
    './components/**/*.{js,ts,jsx,tsx,mdx}',
  ],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        arteria: {
          bg: 'var(--arteria-bg)',
          surface: 'var(--arteria-surface)',
          card: 'var(--arteria-card)',
          border: 'var(--arteria-border)',
          'border-hover': 'var(--arteria-border-hover)',
          accent: '#6366f1',
          'accent-hover': '#818cf8',
          success: '#10b981',
          error: '#ef4444',
          warning: '#f59e0b',
          muted: 'var(--arteria-muted)',
          text: 'var(--arteria-text)',
          'text-secondary': 'var(--arteria-text-secondary)',
        },
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', '-apple-system', 'sans-serif'],
        mono: ['JetBrains Mono', 'Fira Code', 'monospace'],
      },
      fontSize: {
        '2xs': '0.625rem',
      },
      boxShadow: {
        'glow': '0 0 20px rgba(99, 102, 241, 0.15)',
        'glow-sm': '0 0 10px rgba(99, 102, 241, 0.1)',
      },
      animation: {
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
      },
    },
  },
  plugins: [],
};

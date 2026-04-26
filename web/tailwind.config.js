/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        // Mirrors internal/jobposting/internal/hiringintent design tokens
        // (Figr palette — accent reads as indigo despite legacy name).
        accent: {
          DEFAULT: '#5B4CFF',
          hover: '#4A3DE6',
          soft: '#5B4CFF14',
          mid: '#5B4CFF24',
        },
        ink: {
          DEFAULT: '#0A1628',
          sub: '#64748B',
          mute: '#94A3B8',
        },
        line: {
          DEFAULT: '#E2E8F0',
          soft: '#F1F5F9',
        },
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', 'sans-serif'],
        display: ['"Playfair Display"', 'Georgia', 'serif'],
      },
    },
  },
  plugins: [],
};

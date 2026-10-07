/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    "./internal/templates/**/*.html",
    "./web/**/*.html",
    "./*.html",
    "./static/**/*.js",
  ],
  theme: {
    extend: {
      animation: {
        'fade-in': 'fadeIn 0.3s ease-in',
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0', transform: 'translateY(-10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' },
        },
      },
    },
  },
  plugins: [
    require('@tailwindcss/forms'),
    require('@tailwindcss/typography'),
  ],
}
/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./web/templates/**/*.html"],
  theme: {
    extend: {
      colors: {
        white: "#FFFEFA",
        gray: {
          50: "#F8F5EE",
          100: "#F1ECE2",
          200: "#E2DACC",
          300: "#CFC4B2",
          400: "#989D93",
          500: "#6E756B",
          600: "#555D53",
          700: "#3D443C",
          800: "#2E332C",
          900: "#242820"
        },
        blue: {
          50: "#EDF2EC",
          100: "#DDE5DC",
          200: "#C2D0C1",
          300: "#9EB29F",
          400: "#718A74",
          500: "#5C765F",
          600: "#4E6653",
          700: "#34473A",
          800: "#29382F",
          900: "#202B24"
        },
        green: {
          50: "#EDF2EC",
          100: "#DDE5DC",
          200: "#C2D0C1",
          500: "#5C765F",
          600: "#4E6653",
          700: "#34473A",
          800: "#29382F",
          900: "#202B24"
        },
        purple: {
          50: "#EDF2EC",
          100: "#DDE5DC",
          500: "#5C765F",
          600: "#4E6653",
          700: "#34473A"
        },
        red: {
          50: "#F8EFEC",
          100: "#F0DDD7",
          200: "#E4C1B8",
          500: "#B85C47",
          600: "#9B4335",
          700: "#7E352B",
          800: "#652D26"
        },
        rose: {
          50: "#F8EFEC",
          100: "#F0DDD7",
          200: "#E4C1B8",
          500: "#B85C47",
          600: "#9B4335",
          700: "#7E352B",
          800: "#652D26"
        },
        ink: {
          DEFAULT: "#242820",
          muted: "#6E756B"
        },
        paper: {
          DEFAULT: "#F5F1E8",
          raised: "#FFFEFA"
        },
        parchment: "#E8E0D1",
        rule: "#D4CBBB",
        moss: {
          DEFAULT: "#4E6653",
          dark: "#34473A",
          pale: "#DDE5DC"
        },
        clay: "#9B4335"
      },
      fontFamily: {
        display: ['"Iowan Old Style"', '"Palatino Linotype"', "Palatino", "Georgia", "serif"],
        sans: ['"Avenir Next"', "Avenir", '"Segoe UI"', "sans-serif"],
        mono: ["ui-monospace", "SFMono-Regular", "Menlo", "Monaco", "Consolas", "monospace"]
      },
      boxShadow: {
        paper: "0 18px 50px rgba(36, 40, 32, 0.08)"
      }
    }
  },
  plugins: []
}

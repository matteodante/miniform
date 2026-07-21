/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./web/templates/**/*.html", "!./web/templates/demo.html"],
  theme: {
    extend: {
      colors: {
        white: "#FFFFFF",
        gray: {
          50: "#F7F8FC",
          100: "#F1F3F9",
          200: "#E3E6F0",
          300: "#CCD2E2",
          400: "#9CA5BC",
          500: "#6F7891",
          600: "#515A73",
          700: "#373E55",
          800: "#24293D",
          900: "#171A2B"
        },
        blue: {
          50: "#EEF0FF",
          100: "#E1E5FF",
          200: "#C9D0FF",
          300: "#A6B2FF",
          400: "#7487FF",
          500: "#4057F4",
          600: "#3348D5",
          700: "#2D3EC3",
          800: "#29379D",
          900: "#252F7C"
        },
        green: {
          50: "#EAF8F4",
          100: "#D2F0E7",
          200: "#A7E0D1",
          500: "#1D9A7A",
          600: "#147D64",
          700: "#126552",
          800: "#114F43",
          900: "#103F37"
        },
        purple: {
          50: "#F3F0FF",
          100: "#E9E3FF",
          500: "#7257E8",
          600: "#6044CF",
          700: "#4E36AD"
        },
        indigo: {
          50: "#EEF0FF",
          100: "#E1E5FF",
          500: "#4057F4",
          600: "#3348D5",
          700: "#2D3EC3",
          900: "#171A2B"
        },
        red: {
          50: "#FFF0EE",
          100: "#FFDED9",
          200: "#FFBDB5",
          500: "#E65B4F",
          600: "#C9483E",
          700: "#A83A33",
          800: "#89332E"
        },
        rose: {
          50: "#FFF0EE",
          100: "#FFDED9",
          200: "#FFBDB5",
          500: "#E65B4F",
          600: "#C9483E",
          700: "#A83A33",
          800: "#89332E"
        },
        ink: {
          DEFAULT: "#171A2B",
          muted: "#6F7891"
        },
        paper: {
          DEFAULT: "#F1F3F9",
          raised: "#FFFFFF"
        },
        parchment: "#D9DEEC",
        rule: "#D8DDEA",
        moss: {
          DEFAULT: "#4057F4",
          dark: "#2D3EC3",
          pale: "#E1E5FF"
        },
        clay: "#E65B4F"
      },
      fontFamily: {
        display: ['"Avenir Next Condensed"', '"Arial Narrow"', '"Roboto Condensed"', "sans-serif"],
        sans: ['"Avenir Next"', "Avenir", '"Segoe UI"', "system-ui", "sans-serif"],
        mono: ["ui-monospace", "SFMono-Regular", "Menlo", "Monaco", "Consolas", "monospace"]
      },
      boxShadow: {
        paper: "0 24px 70px rgba(23, 26, 43, 0.10)",
        lift: "0 12px 30px rgba(23, 26, 43, 0.08)"
      }
    }
  },
  plugins: []
}

/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./web/templates/**/*.html", "!./web/templates/demo.html"],
  theme: {
    extend: {
      colors: {
        white: "#FFFEFA",
        gray: {
          50: "#F5F1E8",
          100: "#EEE8DD",
          200: "#E8E0D1",
          300: "#D4CBBB",
          400: "#A59E91",
          500: "#686F65",
          600: "#565D54",
          700: "#3E453D",
          800: "#30352F",
          900: "#242820"
        },
        blue: {
          50: "#EFF3F4",
          100: "#E1E9EB",
          200: "#C4D3D7",
          300: "#A1B8BF",
          400: "#76939C",
          500: "#526A78",
          600: "#465C68",
          700: "#3A4D57",
          800: "#304048",
          900: "#26343A"
        },
        green: {
          50: "#EEF3EF",
          100: "#DFE8E1",
          200: "#C4D4C7",
          500: "#4E6653",
          600: "#405845",
          700: "#34473A",
          800: "#29382E",
          900: "#202B24"
        },
        red: {
          50: "#F9EFEC",
          100: "#F2DDD8",
          200: "#E2BAB1",
          500: "#9B4335",
          600: "#873A2F",
          700: "#713129",
          800: "#5D2A23"
        },
        rose: {
          50: "#F9EFEC",
          100: "#F2DDD8",
          200: "#E2BAB1",
          500: "#9B4335",
          600: "#873A2F",
          700: "#713129",
          800: "#5D2A23"
        },
        ink: {
          DEFAULT: "#242820",
          muted: "#686F65"
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
          pale: "#DFE8E1"
        },
        clay: "#9B4335"
      },
      fontFamily: {
        display: ['"Iowan Old Style"', '"Palatino Linotype"', "Palatino", "Georgia", "serif"],
        sans: ['"Avenir Next"', "Avenir", '"Segoe UI"', "system-ui", "sans-serif"],
        mono: ["ui-monospace", "SFMono-Regular", "Menlo", "Monaco", "Consolas", "monospace"]
      },
      borderRadius: {
        lg: "8px",
        xl: "10px",
        "2xl": "10px"
      },
      boxShadow: {
        paper: "none",
        lift: "none"
      }
    }
  },
  plugins: []
}

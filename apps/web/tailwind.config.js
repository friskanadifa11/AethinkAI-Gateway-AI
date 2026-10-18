/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        // Signal palette: deep navy surfaces with a single cyan accent that
        // means "this request is moving".
        ink: {
          950: "#070b16",
          900: "#0b1120",
          850: "#0f172c",
          800: "#131c33",
          700: "#1c2942",
          600: "#2a3a58",
        },
        signal: {
          300: "#7dd3fc",
          400: "#38bdf8",
          500: "#0ea5e9",
          600: "#0284c7",
        },
        ok: "#34d399",
        warn: "#fbbf24",
        bad: "#f87171",
      },
      fontFamily: {
        sans: ["Inter", "system-ui", "-apple-system", "Segoe UI", "sans-serif"],
        mono: ["ui-monospace", "SFMono-Regular", "Menlo", "monospace"],
      },
      boxShadow: {
        panel: "0 1px 0 0 rgba(255,255,255,0.04) inset, 0 12px 32px -18px rgba(0,0,0,0.9)",
        signal: "0 0 0 1px rgba(56,189,248,0.35), 0 0 24px -6px rgba(56,189,248,0.35)",
      },
      keyframes: {
        "pulse-rail": {
          "0%, 100%": { opacity: "0.35" },
          "50%": { opacity: "1" },
        },
        "rise-in": {
          from: { opacity: "0", transform: "translateY(4px)" },
          to: { opacity: "1", transform: "translateY(0)" },
        },
      },
      animation: {
        "pulse-rail": "pulse-rail 2.4s ease-in-out infinite",
        "rise-in": "rise-in 200ms ease-out both",
      },
    },
  },
  plugins: [],
};

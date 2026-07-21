import animate from "tailwindcss-animate";

/** skriptvalley "Dusk" design system (ADR-0009). Palette tokens are OKLCH
 *  channels wrapped as `oklch(var(--x) / <alpha-value>)` so Tailwind opacity
 *  modifiers (e.g. bg-brand/15) inject the alpha; border/input/sidebar-border
 *  are full-color vars (they carry their own alpha). */
const channel = (name) => `oklch(var(--${name}) / <alpha-value>)`;

/** @type {import('tailwindcss').Config} */
export default {
  darkMode: ["class"],
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        border: "var(--border)",
        input: "var(--input)",
        ring: channel("ring"),
        "ring-soft": "var(--ring-soft)",
        background: channel("background"),
        foreground: channel("foreground"),
        primary: {
          DEFAULT: channel("primary"),
          foreground: channel("primary-foreground"),
        },
        secondary: {
          DEFAULT: channel("secondary"),
          foreground: channel("secondary-foreground"),
        },
        destructive: {
          DEFAULT: channel("destructive"),
          foreground: channel("destructive-foreground"),
        },
        muted: {
          DEFAULT: channel("muted"),
          foreground: channel("muted-foreground"),
        },
        accent: {
          DEFAULT: channel("accent"),
          foreground: channel("accent-foreground"),
        },
        popover: {
          DEFAULT: channel("popover"),
          foreground: channel("popover-foreground"),
        },
        card: {
          DEFAULT: channel("card"),
          foreground: channel("card-foreground"),
        },
        brand: {
          DEFAULT: channel("brand"),
          foreground: channel("brand-foreground"),
        },
        highlight: {
          DEFAULT: channel("highlight"),
          foreground: channel("highlight-foreground"),
        },
        // Legible badge/dot foregrounds — light uses the dark *-foreground, dark
        // uses the bright base (index.css swaps them). Full colors, so no alpha.
        "badge-brand-fg": "var(--badge-brand-fg)",
        "badge-hl-fg": "var(--badge-hl-fg)",
        "chart-1": channel("chart-1"),
        "chart-2": channel("chart-2"),
        "chart-3": channel("chart-3"),
        sidebar: {
          DEFAULT: channel("sidebar"),
          foreground: channel("sidebar-foreground"),
          primary: channel("sidebar-primary"),
          accent: channel("sidebar-accent"),
          "accent-foreground": channel("sidebar-accent-foreground"),
          border: "var(--sidebar-border)",
        },
      },
      fontFamily: {
        sans: ["Geist Variable", "Geist", "system-ui", "-apple-system", "sans-serif"],
        display: ["Space Grotesk", "Geist Variable", "system-ui", "sans-serif"],
        mono: ["Geist Mono Variable", "Geist Mono", "ui-monospace", "SFMono-Regular", "monospace"],
      },
      borderRadius: {
        lg: "var(--radius)", // 14px — cards, panels, dialog
        md: "calc(var(--radius) - 4px)", // 10px — buttons, inputs, selects, nav
        sm: "calc(var(--radius) - 6px)", // 8px — badges, chips, menu items
      },
      boxShadow: {
        // Cards carry a hairline ring instead of a border.
        ring: "0 0 0 1px var(--ring-soft)",
        popover: "0 10px 30px rgb(0 0 0 / 0.18)",
        dialog: "0 10px 30px rgb(0 0 0 / 0.25)",
      },
      keyframes: {
        fadeIn: { from: { opacity: "0" }, to: { opacity: "1" } },
        dlgIn: {
          from: { opacity: "0", transform: "translate(-50%, -50%) scale(0.95)" },
          to: { opacity: "1", transform: "translate(-50%, -50%) scale(1)" },
        },
      },
      animation: {
        fadeIn: "fadeIn 0.12s ease",
        dlgIn: "dlgIn 0.15s ease",
      },
    },
  },
  plugins: [animate],
};

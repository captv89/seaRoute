// Tailwind v3 config. Rebuild web/static/tailwind.css after UI changes:
//   cd web/tailwind && npm ci && npm run build
module.exports = {
    content: ['../templates/**/*.gohtml', '../static/script.js'],
    darkMode: "class",
    theme: {
        extend: {
            colors: {
                "surface": "rgb(var(--color-surface) / <alpha-value>)",
                "background": "rgb(var(--color-background) / <alpha-value>)",
                "surface-dim": "rgb(var(--color-surface-dim) / <alpha-value>)",
                "surface-container-lowest": "rgb(var(--color-surface-container-lowest) / <alpha-value>)",
                "surface-container-low": "rgb(var(--color-surface-container-low) / <alpha-value>)",
                "surface-container": "rgb(var(--color-surface-container) / <alpha-value>)",
                "surface-container-high": "rgb(var(--color-surface-container-high) / <alpha-value>)",
                "surface-container-highest": "rgb(var(--color-surface-container-highest) / <alpha-value>)",
                "surface-bright": "rgb(var(--color-surface-bright) / <alpha-value>)",
                "surface-variant": "rgb(var(--color-surface-variant) / <alpha-value>)",
                "primary": "rgb(var(--color-primary) / <alpha-value>)",
                "primary-container": "rgb(var(--color-primary-container) / <alpha-value>)",
                "on-primary": "rgb(var(--color-on-primary) / <alpha-value>)",
                "on-primary-container": "rgb(var(--color-on-primary-container) / <alpha-value>)",
                "secondary": "rgb(var(--color-secondary) / <alpha-value>)",
                "secondary-container": "rgb(var(--color-secondary-container) / <alpha-value>)",
                "on-secondary": "rgb(var(--color-on-secondary) / <alpha-value>)",
                "tertiary": "rgb(var(--color-tertiary) / <alpha-value>)",
                "tertiary-container": "rgb(var(--color-tertiary-container) / <alpha-value>)",
                "on-tertiary": "rgb(var(--color-on-tertiary) / <alpha-value>)",
                "on-surface": "rgb(var(--color-on-surface) / <alpha-value>)",
                "on-surface-variant": "rgb(var(--color-on-surface-variant) / <alpha-value>)",
                "outline": "rgb(var(--color-outline) / <alpha-value>)",
                "outline-variant": "rgb(var(--color-outline-variant) / <alpha-value>)",
                "warning": "rgb(var(--color-warning) / <alpha-value>)",
                "warning-container": "rgb(var(--color-warning-container) / <alpha-value>)",
                "eca-cyan": "rgb(var(--color-eca-cyan) / <alpha-value>)",
                "error": "rgb(var(--color-error) / <alpha-value>)",
                "error-container": "rgb(var(--color-error-container) / <alpha-value>)"
            },
            borderRadius: {
                DEFAULT: "0.125rem",
                sm: "0.125rem",
                md: "0.25rem",
                lg: "0.25rem",
                xl: "0.375rem"
            },
            fontFamily: {
                "body-md": ["JetBrains Mono", "monospace"],
                "body-lg": ["JetBrains Mono", "monospace"],
                "headline-md": ["Inter", "sans-serif"],
                "headline-lg": ["Inter", "sans-serif"],
                "label-lg": ["Inter", "sans-serif"],
                "label-md": ["JetBrains Mono", "monospace"],
                "label-sm": ["JetBrains Mono", "monospace"]
            }
        }
    },
    plugins: [
        require('@tailwindcss/forms'),
        require('@tailwindcss/container-queries')
    ]
};

import js from "@eslint/js";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist", "playwright-report", "test-results", ".features-gen", "src/shared/api/schema.d.ts"] },
  js.configs.recommended,
  ...tseslint.configs.strict,
  {
    files: ["**/*.{ts,tsx,mjs}"],
    languageOptions: { globals: { ...globals.browser, ...globals.node } },
    plugins: { "react-hooks": reactHooks },
    rules: {
      ...reactHooks.configs.recommended.rules,
      "@typescript-eslint/consistent-type-imports": "error",
      "no-console": ["error", { allow: ["warn", "error"] }],
    },
  },
  {
    // Playwright fixtures call `use`, which is not the React hook.
    files: ["e2e/**/*.ts"],
    rules: { "react-hooks/rules-of-hooks": "off" },
  },
);

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwind from "@tailwindcss/vite";

// The webview loads only bundled assets (U15). No remote origin, no CDN — the
// generic RPC bridge S9 will add is only safe because the app IS the only thing
// talking to it, and a remote script would break that.
export default defineConfig({
  plugins: [react(), tailwind()],
  build: { target: "es2022", emptyOutDir: true },
  server: {
    port: 5173,
    strictPort: true,
    // cargo output under src-tauri/target holds tens of thousands of files;
    // watching it exhausts inotify (ENOSPC) and kills `beforeDevCommand`.
    watch: { ignored: ["**/src-tauri/target/**", "**/node_modules/**"] },
  },
  test: {
    environment: "jsdom",
    include: [
      "test/**/*.test.tsx",
      "test/**/*.test.ts",
      // S43-extra T8: the System records panel's tests live beside the panel
      // source (the task's "tests beside them"), so the slice command
      // `npx vitest run src/features/settings/seed` discovers them. The tsconfig
      // already type-checks both `src` and `test`; this only widens the runner.
      "src/**/*.test.tsx",
      "src/**/*.test.ts",
    ],
    setupFiles: ["./test/setup.ts"],
    globals: true,
    // 1,100+ jsdom tests across 100+ files compete for the CPU; the heaviest
    // mount-the-whole-app tests then overrun the default 5s wall-clock budget
    // (observed as "Test timed out in 5000ms" on tests that pass in seconds
    // when run alone). The budget is widened; the assertions are untouched.
    testTimeout: 20000,
    hookTimeout: 20000,
  },
});

import { defineConfig } from "tsup";

export default defineConfig([
  {
    entry: { "core/dispatcher": "src/core/dispatcher.ts" },
    format: ["esm"],
    dts: true,
    sourcemap: true,
    clean: true,
    outDir: "dist",
    external: ["react", "ink"],
    noExternal: ["js-yaml", "picomatch"],
  },
  {
    entry: {
      "cli/app": "src/cli/app.tsx",
      "bin/hookwise": "bin/hookwise.ts",
      "core/feeds/daemon-process": "src/core/feeds/daemon-process.ts",
      index: "src/index.ts",
      "testing/index": "src/testing/index.ts",
    },
    format: ["esm"],
    dts: true,
    sourcemap: true,
    clean: false,
    outDir: "dist",
  },
]);

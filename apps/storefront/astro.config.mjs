import { defineConfig } from "astro/config";
import node from "@astrojs/node";

export default defineConfig({
  server: { port: 4321, host: true },
  output: "hybrid",
  adapter: node({ mode: "standalone" }),
});


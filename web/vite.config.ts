import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      "/api": {
        target: process.env.NOVELFLOW_DEV_API || "http://127.0.0.1:8731",
        changeOrigin: false,
      },
    },
  },
});

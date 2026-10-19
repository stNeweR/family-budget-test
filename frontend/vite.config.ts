import { defineConfig, loadEnv } from 'vite';
import vue from '@vitejs/plugin-vue';

// In docker compose the backend is reachable as http://backend:8080; locally
// it is http://localhost:8080. Override with VITE_PROXY_TARGET if needed.
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const target = env.VITE_PROXY_TARGET || 'http://localhost:8080';

  return {
    plugins: [vue()],
    server: {
      host: true,
      port: 5173,
      proxy: {
        '/api': target,
        '/telegram': target,
      },
    },
  };
});

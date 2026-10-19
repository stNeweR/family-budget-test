import { defineConfig, loadEnv } from 'vite';
import tailwindcss from '@tailwindcss/vite';
import vue from '@vitejs/plugin-vue';

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const target = env.VITE_PROXY_TARGET || 'http://localhost:8080';

  return {
    plugins: [vue(), tailwindcss()],
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

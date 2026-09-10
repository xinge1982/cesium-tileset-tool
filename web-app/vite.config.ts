import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import cesium from 'vite-plugin-cesium'

// https://vite.dev/config/
// @ts-ignore
export default defineConfig(({ command, mode }) => {
  return {
    plugins: [
      vue(),
      //依赖分析插件
      // visualizer({
      //   open: true,
      //   gzipSize: true,
      //   brotliSize: true
      // })
      cesium()],
    define: {
      //define global var
      CESIUM_BASE_URL: JSON.stringify('/cesium')
    }
  }
})

import axios from 'axios'
const http = axios.create({
    baseURL: import.meta.env.VITE_APP_BASE_URL,
    timeout: 30000,
})

http.interceptors.request.use(
    (config) => {
        return config
    },
    (error) => {
        return Promise.reject(error)
    }
)

export default http
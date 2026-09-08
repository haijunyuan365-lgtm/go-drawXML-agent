// 统一保存后端 API 根地址，避免每个请求函数重复拼接地址。
export const API_CONFIG = {
  // Docker 运行时优先读取 window.__ENV；本地开发或构建阶段再读取 process.env。
  BASE_URL: (typeof window !== 'undefined' && window.__ENV?.NEXT_PUBLIC_API_BASE_URL) 
    ? window.__ENV.NEXT_PUBLIC_API_BASE_URL 
    : (process.env.NEXT_PUBLIC_API_BASE_URL || 'http://localhost:8091/api/v1'),
};

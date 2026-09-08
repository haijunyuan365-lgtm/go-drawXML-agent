export const COOKIE_NAME = "ai_agent_login";
export const COOKIE_DAYS = 7;

export interface UserInfo {
  user: string;
  ts: number;
}

// 写入普通浏览器 Cookie。这里只是演示登录，不是真正安全鉴权。
export const setCookie = (name: string, value: string, days: number) => {
  const maxAge = Math.max(0, Math.floor(days * 86400));
  document.cookie = `${name}=${encodeURIComponent(value)}; Max-Age=${maxAge}; Path=/; SameSite=Lax`;
};

// 从 document.cookie 的分号分隔字符串中查找指定 Cookie。
export const getCookie = (name: string): string | null => {
  const cookies = document.cookie ? document.cookie.split("; ") : [];
  for (const item of cookies) {
    const eqIndex = item.indexOf("=");
    const k = eqIndex >= 0 ? item.slice(0, eqIndex) : item;
    const v = eqIndex >= 0 ? item.slice(eqIndex + 1) : "";
    if (k === name) return decodeURIComponent(v);
  }
  return null;
};

// 把 Max-Age 设为 0，让浏览器立即删除 Cookie。
export const deleteCookie = (name: string) => {
  document.cookie = `${name}=; Max-Age=0; Path=/; SameSite=Lax`;
};

// 读取并反序列化登录用户；损坏的 Cookie 直接视为未登录。
export const getUserInfo = (): UserInfo | null => {
  const raw = getCookie(COOKIE_NAME);
  if (!raw) return null;
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
};

// 保存用户名和写入时间，统一使用 COOKIE_NAME。
export const setUserInfo = (user: string) => {
  const payload: UserInfo = { user, ts: Date.now() };
  setCookie(COOKIE_NAME, JSON.stringify(payload), COOKIE_DAYS);
};

// 清除演示登录状态。
export const clearUserInfo = () => {
  deleteCookie(COOKIE_NAME);
};

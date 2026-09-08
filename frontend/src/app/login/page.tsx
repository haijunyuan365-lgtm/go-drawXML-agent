'use client';

import { useState, useEffect } from 'react';
import { useRouter } from 'next/navigation';
import { setUserInfo, getUserInfo, clearUserInfo } from '@/utils/cookie';

export default function Login() {
  const router = useRouter();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [msg, setMsg] = useState({ text: '', type: '' });
  const [isLoggedIn, setIsLoggedIn] = useState(false);
  const [currentUser, setCurrentUser] = useState('');

  // 页面挂载后检查 Cookie；已登录用户不再停留在登录页。
  useEffect(() => {
    const userInfo = getUserInfo();
    if (userInfo && userInfo.user) {
      setIsLoggedIn(true);
      setCurrentUser(userInfo.user);
      // If already logged in, redirect to home
      router.push('/');
    }
  }, [router]);

  // 校验演示账号，成功后写 Cookie 并跳转首页。
  const handleLogin = (e: React.FormEvent) => {
    e.preventDefault();
    setMsg({ text: '', type: '' });

    if (!username || !password) {
      setMsg({ text: '请输入账号与密码。', type: 'error' });
      return;
    }

    if (username !== 'admin' || password !== 'admin') {
      setMsg({ text: '账号或密码错误（演示账号：admin / admin）。', type: 'error' });
      return;
    }

    setUserInfo(username);
    setMsg({ text: '登录成功，正在跳转…', type: 'info' });
    setTimeout(() => {
      router.push('/');
    }, 500);
  };

  // 一键填充演示账号，方便本地调试。
  const handleFillDemo = () => {
    setUsername('admin');
    setPassword('admin');
    setMsg({ text: '已填充演示账号。', type: 'info' });
  };

  // 清除 Cookie，并把页面恢复为未登录状态。
  const handleLogout = () => {
    clearUserInfo();
    setIsLoggedIn(false);
    setCurrentUser('');
    setMsg({ text: '已退出登录，cookie 已清除。', type: 'info' });
  };

  return (
    <div className="min-h-screen flex justify-center items-stretch p-7 theme-bg-gradient">
      <div className="w-full max-w-[1120px] grid grid-cols-1 lg:grid-cols-[1.25fr_0.75fr] gap-[18px]">
        {/* Hero Section */}
        <section className="theme-card rounded-[18px] overflow-hidden relative flex flex-col gap-[18px] p-[28px_28px_22px_28px]">
          <div className="flex items-center gap-3">
            <div className="w-11 h-11 rounded-[14px] grid place-items-center bg-gradient-to-br from-[#62f6c7] to-[#5aa9ff] shadow-[0_10px_24px_rgba(0,0,0,0.4)] text-[rgba(7,10,18,0.92)] font-extrabold text-lg tracking-[0.5px]">
              AI
            </div>
            <div className="flex flex-col gap-1">
              <strong className="text-base leading-[1.1] tracking-[0.2px] text-[rgba(255,255,255,0.92)]">
                AI 智能体工作台 By Ai Agent Scaffold - @peakxy
              </strong>
              <span className="text-xs text-[rgba(255,255,255,0.56)]">更快搭建 · 更稳运行 · 更易运维</span>
            </div>
          </div>

          <h1 className="mt-[6px] text-[30px] leading-[1.2] tracking-[0.2px] text-[rgba(255,255,255,0.92)] font-bold">
            一个能“帮你把事做完”的智能体登录页
          </h1>
          <p className="m-0 text-[rgba(255,255,255,0.72)] leading-[1.7] max-w-[52ch] text-sm">
            左侧展示智能体能力与效果图，右侧进行登录。当前为演示登录：
            账号 <b>admin</b>，密码 <b>admin</b>。登录成功后会在浏览器保存 cookie。
          </p>

          <div className="grid grid-cols-2 gap-3 mt-[6px]">
            {[
              { title: '工具调用', desc: '支持 API / Shell / 文件等执行链路编排' },
              { title: '记忆与上下文', desc: '可配置可审计，减少重复沟通成本' },
              { title: '多模型路由', desc: '按场景选择最合适的模型与策略' },
              { title: '可观测性', desc: '链路、成本、失败原因都能追踪' },
            ].map((item, idx) => (
              <div key={idx} className="border border-[rgba(255,255,255,0.08)] bg-[rgba(255,255,255,0.04)] rounded-[14px] p-3 flex gap-[10px] items-start">
                <div className="w-[10px] h-[10px] rounded-full mt-[5px] flex-shrink-0 bg-gradient-to-br from-[#62f6c7] to-[#5aa9ff] shadow-[0_0_0_4px_rgba(98,246,199,0.08)]"></div>
                <div>
                  <b className="block text-[13px] mb-[3px] text-[rgba(255,255,255,0.92)]">{item.title}</b>
                  <span className="block text-xs text-[rgba(255,255,255,0.56)] leading-[1.5]">{item.desc}</span>
                </div>
              </div>
            ))}
          </div>

          <div className="mt-[10px] rounded-[16px] overflow-hidden border border-[rgba(255,255,255,0.10)] bg-[rgba(0,0,0,0.24)] h-[340px] relative">
             {/* Placeholder for Hero Image - mimicking the original svg placeholder */}
             <div className="w-full h-full flex items-center justify-center text-[rgba(255,255,255,0.2)] text-sm">
                AI 智能体效果图
             </div>
          </div>
        </section>

        {/* Login Form Section */}
        <section className="p-[28px] flex flex-col justify-center gap-[14px]">
          <div className="theme-card rounded-[16px] p-5">
            <h2 className="m-0 mb-[6px] text-[18px] text-[rgba(255,255,255,0.92)] font-bold">登录</h2>
            <p className="m-0 mb-4 text-[rgba(255,255,255,0.56)] text-xs leading-[1.5]">
              演示账号：admin / admin（可在页面脚本中替换成真实鉴权接口）
            </p>

            {!isLoggedIn ? (
              <form onSubmit={handleLogin} autoComplete="on">
                <div className="flex flex-col gap-2 mb-3">
                  <label htmlFor="username" className="text-xs text-[rgba(255,255,255,0.72)] tracking-[0.2px]">账号</label>
                  <input
                    id="username"
                    type="text"
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                    placeholder="请输入账号"
                    autoComplete="username"
                    className="w-full rounded-[12px] theme-input p-3 outline-none transition-all duration-180 text-sm"
                  />
                </div>

                <div className="flex flex-col gap-2 mb-3">
                  <label htmlFor="password" className="text-xs text-[rgba(255,255,255,0.72)] tracking-[0.2px]">密码</label>
                  <input
                    id="password"
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    placeholder="请输入密码"
                    autoComplete="current-password"
                    className="w-full rounded-[12px] theme-input p-3 outline-none transition-all duration-180 text-sm"
                  />
                </div>

                <div className="flex gap-[10px] items-center justify-between mt-[6px]">
                  <button type="submit" className="theme-btn rounded-[12px] p-[11px_14px] font-bold cursor-pointer border-0 transition-transform active:translate-y-[1px] active:brightness-[0.98] text-sm">
                    登录并保存 Cookie
                  </button>
                  <button type="button" onClick={handleFillDemo} className="theme-btn-secondary rounded-[12px] p-[11px_14px] font-semibold cursor-pointer transition-transform active:translate-y-[1px] active:brightness-[0.98] text-sm">
                    填充演示账号
                  </button>
                </div>
              </form>
            ) : (
              <div className="flex gap-[10px] items-center justify-between p-3 border border-dashed border-[rgba(255,255,255,0.18)] rounded-[12px] bg-[rgba(255,255,255,0.04)] mt-3">
                <div>
                  <strong className="block text-[13px] text-[rgba(255,255,255,0.92)]">已登录：{currentUser}</strong>
                  <span className="block text-xs text-[rgba(255,255,255,0.56)] mt-[2px]">欢迎回来</span>
                </div>
                <button onClick={handleLogout} className="theme-btn-secondary rounded-[12px] p-[8px_12px] font-semibold cursor-pointer text-xs">
                  退出
                </button>
              </div>
            )}

            <div className={`min-h-[18px] text-xs mt-2 ${msg.type === 'error' ? 'text-[#ff5a7a]' : 'text-[rgba(255,255,255,0.56)]'}`}>
              {msg.text}
            </div>
          </div>

          <div className="mt-[14px] text-[rgba(255,255,255,0.35)] text-xs text-center">
            © AI Agent Scaffold · Next.js 页面示例
          </div>
        </section>
      </div>
    </div>
  );
}

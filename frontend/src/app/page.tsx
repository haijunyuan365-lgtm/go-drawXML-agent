'use client';

import { DrawIoEmbed, DrawIoEmbedRef } from 'react-drawio';
import { useRef, useState, useEffect } from 'react';
import { useRouter } from 'next/navigation';
import { getUserInfo, clearUserInfo } from '@/utils/cookie';
import { agentApi } from '@/api/agent';
import { AiAgentConfigResponseDTO } from '@/types/api';

// Message type definition
type Message = {
  id: string;
  role: 'user' | 'agent';
  content: string;
  timestamp: number;
};

// 用轻量特征判断模型输出是否像 draw.io XML；不是严格 XML 校验器。
const isDrawIoXmlContent = (content: string) => {
  const trimmed = content.trim();
  return trimmed.startsWith('<mxfile') ||
    trimmed.startsWith('<mxGraphModel') ||
    trimmed.includes('<mxCell') ||
    trimmed.includes('<diagram');
};

// Elegant SVG Icons with consistent styling
const Icons = {
  Chat: ({ className }: { className?: string }) => (
    <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
      <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"></path>
    </svg>
  ),
  Close: ({ className }: { className?: string }) => (
    <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
      <line x1="18" y1="6" x2="6" y2="18"></line>
      <line x1="6" y1="6" x2="18" y2="18"></line>
    </svg>
  ),
  Send: ({ className }: { className?: string }) => (
    <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
      <line x1="22" y1="2" x2="11" y2="13"></line>
      <polygon points="22 2 15 22 11 13 2 9 22 2"></polygon>
    </svg>
  ),
  User: ({ className }: { className?: string }) => (
    <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
      <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"></path>
      <circle cx="12" cy="7" r="4"></circle>
    </svg>
  ),
  Bot: ({ className }: { className?: string }) => (
    <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
      <path d="M12 2a2 2 0 0 1 2 2v2a2 2 0 0 1-2 2 2 2 0 0 1-2-2V4a2 2 0 0 1 2-2z"></path>
      <path d="M4 11v6a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2z"></path>
      <path d="M9 22v-3"></path>
      <path d="M15 22v-3"></path>
    </svg>
  ),
  Download: ({ className }: { className?: string }) => (
    <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
      <polyline points="7 10 12 15 17 10"></polyline>
      <line x1="12" y1="15" x2="12" y2="3"></line>
    </svg>
  ),
  Sparkles: ({ className }: { className?: string }) => (
    <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
      <path d="m12 3-1.912 5.813a2 2 0 0 1-1.275 1.275L3 12l5.813 1.912a2 2 0 0 1 1.275 1.275L12 21l1.912-5.813a2 2 0 0 1 1.275-1.275L21 12l-5.813-1.912a2 2 0 0 1-1.275-1.275L12 3Z" />
    </svg>
  ),
  Logout: ({ className }: { className?: string }) => (
    <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
      <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"></path>
      <polyline points="16 17 21 12 16 7"></polyline>
      <line x1="21" y1="12" x2="9" y2="12"></line>
    </svg>
  ),
  Layers: ({ className }: { className?: string }) => (
    <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
      <polygon points="12 2 2 7 12 12 22 7 12 2"></polygon>
      <polyline points="2 17 12 22 22 17"></polyline>
      <polyline points="2 12 12 17 22 12"></polyline>
    </svg>
  ),
  Loader: ({ className }: { className?: string }) => (
    <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={`animate-spin ${className}`}>
      <path d="M21 12a9 9 0 1 1-6.219-8.56"></path>
    </svg>
  ),
  Plus: ({ className }: { className?: string }) => (
    <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
      <line x1="12" y1="5" x2="12" y2="19"></line>
      <line x1="5" y1="12" x2="19" y2="12"></line>
    </svg>
  ),
  Trash: ({ className }: { className?: string }) => (
    <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
      <polyline points="3 6 5 6 21 6"></polyline>
      <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
    </svg>
  ),
  MessageSquare: ({ className }: { className?: string }) => (
    <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
      <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"></path>
    </svg>
  )
};

// 前端会话：既保存聊天消息，也保存该会话独立的画布 XML。
// id 是浏览器本地会话 ID；backendSessionId 是 Go Runner 会话 ID。
interface Session {
  id: string;
  backendSessionId?: string;
  title: string;
  messages: Message[];
  drawIoXml: string | null;
  lastModified: number;
}

export default function Home() {
  const router = useRouter();
  const [imgData, setImgData] = useState<string | null>(null);
  const drawioRef = useRef<DrawIoEmbedRef>(null);
  
  // User State
  const [currentUser, setCurrentUser] = useState('');

  // Chat State
  const [isChatOpen, setIsChatOpen] = useState(true);
  const [messages, setMessages] = useState<Message[]>([
    {
      id: '1',
      role: 'agent',
      content: '你好！我是你的智能架构助手。请选择一个智能体开始对话。',
      timestamp: Date.now()
    }
  ]);
  const [inputValue, setInputValue] = useState('');
  const [isSending, setIsSending] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  // Context State
  const [useHistoryContext, setUseHistoryContext] = useState(false);
  const [lastExportedData, setLastExportedData] = useState<{data: string, timestamp: number} | null>(null);
  const isExportingForChatRef = useRef(false);
  const isAutosaveRef = useRef(false);
  const pendingMessageRef = useRef('');
  const [isDrawIoReady, setIsDrawIoReady] = useState(false);
  const initialLoadDoneRef = useRef(false);

  // Agent State
  const [agents, setAgents] = useState<AiAgentConfigResponseDTO[]>([]);
  const [selectedAgentId, setSelectedAgentId] = useState('');
  const [sessionId, setSessionId] = useState('');

  // Rename State
  const [isRenameModalOpen, setIsRenameModalOpen] = useState(false);
  const [renamingSessionId, setRenamingSessionId] = useState<string | null>(null);
  const [newSessionTitle, setNewSessionTitle] = useState('');

  // Session Management State
  const [sessions, setSessions] = useState<Session[]>([]);
  const [currentSessionId, setCurrentSessionId] = useState<string | null>(null);
  const currentSessionRef = useRef(currentSessionId);

  // 把最新的前端会话 ID 同步到 ref，供异步请求完成后判断用户是否已切换会话。
  useEffect(() => {
    currentSessionRef.current = currentSessionId;
  }, [currentSessionId]);

  // Draw.io iframe 准备完成后，只恢复一次当前会话的历史 XML。
  useEffect(() => {
    if (!initialLoadDoneRef.current && isDrawIoReady && currentSessionId && sessions.length > 0) {
      const session = sessions.find(s => s.id === currentSessionId);
      if (session && session.drawIoXml && drawioRef.current) {
        drawioRef.current.load({ xml: session.drawIoXml });
      }
      initialLoadDoneRef.current = true;
    }
  }, [isDrawIoReady, currentSessionId, sessions]);

  // 首次进入页面时恢复浏览器中的全部绘图会话；没有历史则创建默认会话。
  useEffect(() => {
    const savedSessions = localStorage.getItem('drawio_sessions');
    if (savedSessions) {
      try {
        const parsed = JSON.parse(savedSessions);
        setSessions(parsed);
        if (parsed.length > 0) {
          // Load the most recent session (first one if sorted by lastModified desc)
          const mostRecent = parsed.sort((a: Session, b: Session) => b.lastModified - a.lastModified)[0];
          setCurrentSessionId(mostRecent.id);
          setMessages(mostRecent.messages);
          // Note: Draw.io XML loading happens after drawioRef is ready or when we switch
        } else {
            createNewSession(true);
        }
      } catch (e) {
        console.error('Failed to parse sessions:', e);
        createNewSession(true);
      }
    } else {
      createNewSession(true);
    }
  }, []);

  // sessions 任意变化后整体持久化，刷新浏览器也能恢复聊天和画布。
  useEffect(() => {
    if (sessions.length > 0) {
      try {
        localStorage.setItem('drawio_sessions', JSON.stringify(sessions));
      } catch (e) {
        console.error('Failed to save sessions to localStorage:', e);
      }
    }
  }, [sessions]);

  // 把页面当前 messages/sessionId 回写进对应的前端 Session。
  useEffect(() => {
    if (currentSessionId) {
      setSessions(prev => prev.map(session => {
        if (session.id === currentSessionId) {
          return {
            ...session,
            messages,
            backendSessionId: sessionId,
            // Update title if it's the default "New Chat" and we have a user message
            title: session.title === 'New Chat' && messages.find(m => m.role === 'user') 
              ? (messages.find(m => m.role === 'user')?.content.slice(0, 20) || 'New Chat')
              : session.title
          };
        }
        return session;
      }));
    }
  }, [messages, currentSessionId, sessionId]);

  // 创建新的前端会话；backendId 可为空，真正发消息时再懒创建后端会话。
  const createNewSession = (isInitial = false, backendId = '') => {
    const newSession: Session = {
      id: Date.now().toString(),
      backendSessionId: backendId,
      title: 'New Chat',
      messages: [{
        id: Date.now().toString(),
        role: 'agent',
        content: '你好！我是你的智能架构助手。请选择一个智能体开始对话。',
        timestamp: Date.now()
      }],
      drawIoXml: null,
      lastModified: Date.now()
    };

    setSessions(prev => [newSession, ...prev]);
    setCurrentSessionId(newSession.id);
    setMessages(newSession.messages);
    setSessionId(backendId);
    
    if (!isInitial && drawioRef.current) {
      drawioRef.current.load({ xml: '' }); // Clear diagram
    }
  };

  // 点击左侧历史记录时切换前端会话。
  const handleSwitchSession = (targetSessionId: string) => {
    if (targetSessionId === currentSessionId) return;
    loadSession(targetSessionId);
  };

  // 同时恢复消息、后端 sessionId 和该会话的 Draw.io XML。
  const loadSession = (targetSessionId: string) => {
    const session = sessions.find(s => s.id === targetSessionId);
    if (session) {
        setCurrentSessionId(targetSessionId);
        setMessages(session.messages);
        setSessionId(session.backendSessionId || '');
        if (drawioRef.current && session.drawIoXml) {
            drawioRef.current.load({ xml: session.drawIoXml });
        } else if (drawioRef.current) {
            drawioRef.current.load({ xml: '' });
        }
    }
  };

  // 删除浏览器本地会话；若删的是当前会话，则自动切换或新建会话。
  const handleDeleteSession = (e: React.MouseEvent, sessionIdToDelete: string) => {
    e.stopPropagation();
    const newSessions = sessions.filter(s => s.id !== sessionIdToDelete);
    setSessions(newSessions);
    localStorage.setItem('drawio_sessions', JSON.stringify(newSessions));

    if (currentSessionId === sessionIdToDelete) {
        if (newSessions.length > 0) {
            loadSession(newSessions[0].id);
        } else {
            createNewSession();
        }
    }
  };

  // 双击会话打开重命名弹窗。
  const handleDoubleClickSession = (session: Session) => {
    setRenamingSessionId(session.id);
    setNewSessionTitle(session.title);
    setIsRenameModalOpen(true);
  };

  // 保存新的会话标题，不改变消息和画布内容。
  const handleRenameSave = () => {
    if (renamingSessionId && newSessionTitle.trim()) {
      setSessions(prev => prev.map(s => 
        s.id === renamingSessionId ? { ...s, title: newSessionTitle.trim() } : s
      ));
      setIsRenameModalOpen(false);
      setRenamingSessionId(null);
      setNewSessionTitle('');
    }
  };

  // 手动导出 xmlsvg，结果统一由 DrawIoEmbed 的 onExport 回调接收。
  const exportDiagram = () => {
    if (drawioRef.current) {
      drawioRef.current.exportDiagram({
        format: 'xmlsvg'
      });
    }
  };

  // 新消息出现时滚动到聊天列表底部。
  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  };

  useEffect(() => {
    scrollToBottom();
  }, [messages, isChatOpen]);

  // 首页初始化：检查登录状态，并从 Go 后端加载可用 Agent 列表。
  useEffect(() => {
    const userInfo = getUserInfo();
    if (!userInfo || !userInfo.user) {
      router.push('/login');
      return;
    }
    setCurrentUser(userInfo.user);

    // Load Agents
    const loadAgents = async () => {
      try {
        const res = await agentApi.queryAiAgentConfigList();
        setAgents(res.data || []);
        if (res.data && res.data.length > 0) {
          // Try to restore last agent or default to first
          const lastAgentId = localStorage.getItem('ai_agent_last_agent');
          if (lastAgentId && res.data.find(a => a.agentId === lastAgentId)) {
            setSelectedAgentId(lastAgentId);
          } else {
            setSelectedAgentId(res.data[0].agentId);
          }
        }
      } catch (error) {
        console.error('Failed to load agents:', error);
        setMessages(prev => [...prev, {
          id: Date.now().toString(),
          role: 'agent',
          content: '加载智能体列表失败，请检查后端服务是否启动。',
          timestamp: Date.now()
        }]);
      }
    };
    loadAgents();
  }, [router]);

  // 清除演示登录 Cookie，并返回登录页。
  const handleLogout = () => {
    clearUserInfo();
    router.push('/login');
  };

  // 切换 Agent 后旧后端会话不能复用，因此清空 sessionId。
  const handleAgentChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const newAgentId = e.target.value;
    setSelectedAgentId(newAgentId);
    setSessionId(''); // Reset session when agent changes
    localStorage.setItem('ai_agent_last_agent', newAgentId);
  };

  // 调后端创建 Runner 会话，再创建对应的前端绘图会话。
  const finalizeNewChat = async () => {
    if (!selectedAgentId || !currentUser) return;
    
    try {
        const res = await agentApi.createSession(selectedAgentId, currentUser);
        createNewSession(false, res.data.sessionId);
        setInputValue('');
    } catch (error) {
        console.error('Failed to create new session:', error);
    }
  };

  // “新增聊天”按钮入口。
  const handleNewChat = async () => {
     finalizeNewChat();
  };

  // 保留当前前端会话条目，但重新申请后端 sessionId 并清空聊天消息。
  const handleRestartSession = async () => {
    if (!selectedAgentId || !currentUser) return;

    if (!currentSessionId) {
      finalizeNewChat();
      return;
    }
    
    try {
        const res = await agentApi.createSession(selectedAgentId, currentUser);
        const newBackendId = res.data.sessionId;
        
        const initialMsg: Message = {
          id: Date.now().toString(),
          role: 'agent',
          content: '你好！我是你的智能架构助手。请选择一个智能体开始对话。',
          timestamp: Date.now()
        };

        setSessionId(newBackendId);
        setMessages([initialMsg]);
        setInputValue('');

        setSessions(prev => prev.map(session => {
          if (session.id === currentSessionId) {
            return {
              ...session,
              backendSessionId: newBackendId,
              messages: [initialMsg],
              lastModified: Date.now()
            };
          }
          return session;
        }));
    } catch (error) {
        console.error('Failed to restart session:', error);
    }
  };

  // 真正的发送主流程：显示用户消息、确保后端会话、调用 /chat、处理文本或 XML。
// displayContent 用于界面展示；apiContent 可额外包含画布 XML 上下文。
  const performSendMessage = async (displayContent: string, apiContent: string) => {
    if (!selectedAgentId) {
      setMessages(prev => [...prev, {
        id: Date.now().toString(),
        role: 'agent',
        content: '请先选择一个智能体。',
        timestamp: Date.now()
      }]);
      setIsSending(false);
      return;
    }

    const userMsg: Message = {
      id: Date.now().toString(),
      role: 'user',
      content: displayContent,
      timestamp: Date.now()
    };
    setMessages(prev => [...prev, userMsg]);

    try {
      // 1. Ensure Session
      let activeBackendSessionId = sessionId;
      if (!activeBackendSessionId) {
        const sessionRes = await agentApi.createSession(selectedAgentId, currentUser);
        activeBackendSessionId = sessionRes.data.sessionId;
        setSessionId(activeBackendSessionId);
      }

      // Update session lastModified
      setSessions(prev => prev.map(session => {
        if (session.id === currentSessionId) {
          return { ...session, lastModified: Date.now() };
        }
        return session;
      }));

      // 2. Send Message
      const chatRes = await agentApi.chat({
        agentId: selectedAgentId,
        userId: currentUser,
        sessionId: activeBackendSessionId,
        message: apiContent
      });

      const { content } = chatRes.data;

      // Go backend returns { content }; infer whether the content is draw.io XML.
      if (!isDrawIoXmlContent(content)) {
        const agentMsg: Message = {
          id: (Date.now() + 1).toString(),
          role: 'agent',
          content: content,
          timestamp: Date.now()
        };
        setMessages(prev => [...prev, agentMsg]);
      } else {
        // Save to session immediately (always update the session that initiated the request)
        setSessions(prev => prev.map(session => {
          if (session.id === currentSessionId) {
            return { 
              ...session, 
              drawIoXml: content,
              lastModified: Date.now() 
            };
          }
          return session;
        }));

        // Render only if still on the same session
        if (drawioRef.current && currentSessionId === currentSessionRef.current) {
          try {
            drawioRef.current.load({
              xml: content
            });
          } catch (e) {
            console.error('Failed to load diagram:', e);
          }
        }
      }

    } catch (error) {
      console.error('Chat error:', error);
      const errorMsg: Message = {
        id: (Date.now() + 1).toString(),
        role: 'agent',
        content: error instanceof Error ? `Error: ${error.message}` : '发送失败，请重试。',
        timestamp: Date.now()
      };
      setMessages(prev => [...prev, errorMsg]);
    } finally {
      setIsSending(false);
    }
  };

  // 发送入口：普通模式直接发送；上下文模式先触发 Draw.io 异步导出 XML。
  const handleSendMessage = async () => {
    if (!inputValue.trim() || isSending) return;
    
    const content = inputValue;
    setInputValue('');
    setIsSending(true);

    if (useHistoryContext && drawioRef.current && isDrawIoReady) {
        isExportingForChatRef.current = true;
        pendingMessageRef.current = content;
        try {
            drawioRef.current.exportDiagram({
                 format: 'xml' as any
             });
        } catch (e) {
            console.error("Export failed", e);
            performSendMessage(content, content);
        }
    } else {
        performSendMessage(content, content);
    }
  };

  // 同一个 onExport 回调承担三种任务：聊天上下文导出、自动保存回退、手动导出预览。
  useEffect(() => {
    if (!lastExportedData) return;
    
    if (isExportingForChatRef.current) {
        isExportingForChatRef.current = false;
        const xml = lastExportedData.data;
        const content = pendingMessageRef.current;
        const apiContent = `[Context: Current Draw.io XML]\n\`\`\`xml\n${xml}\n\`\`\`\n\n${content}`;
        performSendMessage(content, apiContent);
        return;
    }
    
    // Autosave handling
    if (isAutosaveRef.current) {
        isAutosaveRef.current = false;
        const xml = lastExportedData.data;
        setSessions(prev => prev.map(s => {
            if (s.id === currentSessionId) {
                return { ...s, drawIoXml: xml };
            }
            return s;
        }));
        return;
    }

    // Manual Export
    setImgData(lastExportedData.data);
  }, [lastExportedData]);

  // Ctrl+Enter 或 Command+Enter 快捷发送。
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      handleSendMessage();
    }
  };

  const quickActions = [
    { label: '绘制h5端登录流程图', text: '请帮我绘制一个H5端的登录流程图，包含用户输入手机号、获取验证码、验证登录等步骤。' },
    { label: '绘制电商购物流程图', text: '请帮我绘制一个电商购物流程图，包含商品浏览、加入购物车、下单、支付、发货等环节。' }
  ];

  return (
    <div className="flex flex-col h-screen w-full overflow-hidden bg-slate-50 text-slate-900 font-sans">
      {/* Header - Minimal & Clean */}
      <div className="h-14 px-6 bg-white border-b border-slate-200 flex items-center justify-between shrink-0 z-40 relative">
        <div className="flex items-center gap-3">
          <div className="bg-indigo-600 p-1.5 rounded-lg shadow-sm shadow-indigo-200">
            <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
               <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
               <circle cx="8.5" cy="8.5" r="1.5"></circle>
               <polyline points="21 15 16 10 5 21"></polyline>
            </svg>
          </div>
          <h1 className="text-lg font-bold text-slate-800 tracking-tight">ai + draw.io <span className="text-slate-400 font-normal text-sm ml-2">@peakxy</span></h1>
        </div>
        
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2 px-3 py-1.5 bg-slate-50 rounded-full border border-slate-200 shadow-sm">
             <div className="w-2 h-2 rounded-full bg-green-500 shadow-[0_0_8px_rgba(34,197,94,0.4)]"></div>
             <span className="text-xs font-semibold text-slate-600">{currentUser || 'Guest'}</span>
          </div>

          <div className="h-6 w-px bg-slate-200 mx-1"></div>

          <button 
            onClick={exportDiagram}
            className="flex items-center gap-2 px-4 py-1.5 bg-white border border-slate-200 text-slate-600 rounded-lg hover:bg-slate-50 hover:border-slate-300 hover:text-slate-900 transition-all text-sm font-medium shadow-sm active:scale-95"
          >
            <Icons.Download className="w-4 h-4" />
            Export
          </button>

          <button
             onClick={handleLogout}
             className="p-2 text-slate-400 hover:text-red-600 hover:bg-red-50 rounded-lg transition-colors"
             title="Logout"
          >
            <Icons.Logout />
          </button>
          
          {!isChatOpen && (
            <button
              onClick={() => setIsChatOpen(true)}
              className="p-2 text-indigo-600 bg-indigo-50 hover:bg-indigo-100 rounded-lg transition-colors border border-indigo-100"
              title="Open Assistant"
            >
              <Icons.Chat />
            </button>
          )}
        </div>
      </div>
      
      {/* Main Layout */}
      <div className="flex flex-1 w-full overflow-hidden relative">
        {/* Sessions Sidebar */}
        <div className="w-64 bg-white text-slate-600 flex flex-col border-r border-slate-200 shrink-0 z-30">
          <div className="h-14 px-4 flex items-center justify-between border-b border-slate-100 shrink-0">
             <span className="font-semibold text-slate-800 flex items-center gap-2">
                <Icons.MessageSquare className="w-4 h-4 text-indigo-600" />
                绘图记录
             </span>
             <button 
                onClick={handleNewChat}
                className="p-1.5 text-slate-400 hover:text-indigo-600 hover:bg-indigo-50 rounded-md transition-all"
                title="New Chat"
             >
                <Icons.Plus className="w-5 h-5" />
             </button>
          </div>
          <div className="flex-1 overflow-y-auto p-2 space-y-1 scrollbar-thin scrollbar-thumb-slate-200 scrollbar-track-transparent">
             {[...sessions].sort((a, b) => b.lastModified - a.lastModified).map(session => (
                <div 
                  key={session.id}
                  onClick={() => handleSwitchSession(session.id)}
                  onDoubleClick={(e) => { e.stopPropagation(); handleDoubleClickSession(session); }}
                  className={`
                    group flex items-center gap-3 px-3 py-3 rounded-lg cursor-pointer transition-all border border-transparent
                    ${currentSessionId === session.id 
                      ? 'bg-indigo-50 text-indigo-700 border-indigo-100 shadow-sm' 
                      : 'hover:bg-slate-50 text-slate-600 hover:text-slate-900'
                    }
                  `}
                >
                  <div className="flex-1 min-w-0">
                     <div className={`text-sm font-medium truncate ${currentSessionId === session.id ? 'text-indigo-700' : 'text-slate-700 group-hover:text-slate-900'}`}>
                        {session.title}
                     </div>
                     <div className={`text-[10px] mt-0.5 ${currentSessionId === session.id ? 'text-indigo-400' : 'text-slate-400'}`}>
                        {new Date(session.lastModified).toLocaleDateString()} {new Date(session.lastModified).toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'})}
                     </div>
                  </div>
                  <button
                    onClick={(e) => handleDeleteSession(e, session.id)}
                    className={`
                      p-1.5 rounded-md transition-all opacity-0 group-hover:opacity-100
                      ${currentSessionId === session.id 
                        ? 'hover:bg-indigo-100 text-indigo-400 hover:text-indigo-700' 
                        : 'hover:bg-red-50 text-slate-400 hover:text-red-500'
                      }
                    `}
                    title="Delete"
                  >
                    <Icons.Trash className="w-4 h-4" />
                  </button>
                </div>
             ))}
             {sessions.length === 0 && (
                <div className="text-center py-10 text-xs text-slate-400">
                    No history yet
                </div>
             )}
          </div>
        </div>

        {/* Draw.io Canvas Area */}
        <div className="flex-1 relative bg-slate-50 h-full flex flex-col">
          <div className="flex-1 m-3 rounded-2xl overflow-hidden border border-slate-200 shadow-sm bg-white ring-1 ring-slate-100">
            <DrawIoEmbed 
              ref={drawioRef}
              autosave={true}
              onAutoSave={(data) => {
                if (currentSessionId && isDrawIoReady && !isExportingForChatRef.current) {
                   // Prefer using the XML directly from the autosave event if available
                   if (data && typeof data === 'object' && 'xml' in data) {
                       const xmlContent = (data as any).xml;
                       setSessions(prev => prev.map(s => {
                           if (s.id === currentSessionId) {
                               return { ...s, drawIoXml: xmlContent };
                           }
                           return s;
                       }));
                   } else {
                       // Fallback to export if no XML provided in event
                        isAutosaveRef.current = true;
                        drawioRef.current?.exportDiagram({ format: 'xml' as any });
                    }
                }
              }}
              onLoad={() => setIsDrawIoReady(true)}
              onExport={(data) => setLastExportedData({ data: data.data, timestamp: Date.now() })}
              urlParameters={{
                ui: 'atlas', // More modern UI theme for draw.io
                spin: true,
                libraries: true,
                saveAndExit: false,
                noSaveBtn: true,
                noExitBtn: true
              }}
            />
          </div>
        </div>

        {/* Chat Sidebar - Modern & Elegant */}
        <div 
          className={`
            border-l border-slate-200 bg-white flex flex-col transition-all duration-300 ease-[cubic-bezier(0.25,0.1,0.25,1)]
            ${isChatOpen ? 'w-[380px] translate-x-0' : 'w-0 translate-x-full opacity-0 overflow-hidden'}
            shadow-xl z-20
          `}
        >
          {/* Chat Header */}
          <div className="h-14 px-5 border-b border-slate-100 flex items-center justify-between shrink-0 bg-white/80 backdrop-blur-sm sticky top-0 z-10">
            <div className="flex items-center gap-3 flex-1 min-w-0">
              <div className="flex items-center justify-center w-8 h-8 rounded-full bg-gradient-to-br from-indigo-500 to-purple-600 text-white shadow-md shadow-indigo-200 shrink-0 ring-2 ring-white">
                <Icons.Sparkles className="w-4 h-4" />
              </div>
              <div className="flex-1 min-w-0">
                <select 
                  value={selectedAgentId} 
                  onChange={handleAgentChange}
                  className="w-full bg-transparent text-sm font-bold text-slate-800 focus:outline-none cursor-pointer truncate appearance-none pr-4"
                  style={{ backgroundImage: 'none' }}
                >
                  {agents.length === 0 && <option value="">Loading agents...</option>}
                  {agents.map(agent => (
                    <option key={agent.agentId} value={agent.agentId}>
                      {agent.agentName}
                    </option>
                  ))}
                </select>
                <div className="flex items-center gap-1.5 mt-0.5">
                   <span className="w-1.5 h-1.5 rounded-full bg-green-500 animate-pulse"></span>
                   <span className="text-[10px] text-slate-500 font-medium leading-tight">AI Assistant Online</span>
                </div>
              </div>
            </div>
            <div className="flex items-center gap-1">
                <button 
                  onClick={() => setIsChatOpen(false)}
                  className="p-1.5 text-slate-400 hover:text-slate-600 hover:bg-slate-100 rounded-md transition-all shrink-0"
                >
                  <Icons.Close className="w-5 h-5" />
                </button>
            </div>
          </div>

          {/* Messages Area */}
          <div className="flex-1 overflow-y-auto p-5 space-y-6 bg-slate-50/50 scrollbar-thin scrollbar-thumb-slate-200 scrollbar-track-transparent">
            {messages.map((msg) => (
              <div 
                key={msg.id} 
                className={`flex gap-3 ${msg.role === 'user' ? 'flex-row-reverse' : 'flex-row'}`}
              >
                <div className={`
                  shrink-0 w-8 h-8 rounded-full flex items-center justify-center shadow-sm mt-1 ring-2 ring-white
                  ${msg.role === 'user' 
                    ? 'bg-indigo-100 text-indigo-600' 
                    : 'bg-white text-indigo-500 border border-slate-100'
                  }
                `}>
                  {msg.role === 'user' ? <Icons.User className="w-5 h-5" /> : <Icons.Bot className="w-5 h-5" />}
                </div>
                
                <div className="flex flex-col max-w-[85%]">
                    <span className={`text-[10px] mb-1.5 font-medium ${msg.role === 'user' ? 'text-right text-slate-400' : 'text-left text-slate-400'}`}>
                        {msg.role === 'user' ? 'You' : 'Agent'}
                    </span>
                    <div 
                    className={`
                        p-3.5 text-sm leading-relaxed shadow-sm whitespace-pre-wrap
                        ${msg.role === 'user' 
                        ? 'bg-indigo-600 text-white rounded-2xl rounded-tr-sm shadow-indigo-200' 
                        : 'bg-white border border-slate-200 text-slate-700 rounded-2xl rounded-tl-sm shadow-sm'
                        }
                    `}
                    >
                    {msg.content}
                    </div>
                </div>
              </div>
            ))}
            <div ref={messagesEndRef} />
          </div>

          {/* Input Area */}
          <div className="p-4 bg-white border-t border-slate-100 shrink-0 relative z-20 shadow-[0_-4px_6px_-1px_rgba(0,0,0,0.02)]">
            {/* Quick Actions - Only show when chat is empty (just greeting) */}
            {messages.length <= 1 && (
              <div className="flex flex-wrap gap-2 mb-3 px-1 animate-in fade-in slide-in-from-bottom-2 duration-300">
                {quickActions.map((action, idx) => (
                  <button
                    key={idx}
                    onClick={() => setInputValue(action.text)}
                    className="text-xs px-3 py-1.5 bg-indigo-50 text-indigo-600 rounded-full hover:bg-indigo-100 transition-colors border border-indigo-100 font-medium shadow-sm"
                  >
                    {action.label}
                  </button>
                ))}
              </div>
            )}

            {/* Context Toolbar */}
            <div className="flex items-center gap-2 mb-2 px-1">
                <button
                    onClick={() => setUseHistoryContext(!useHistoryContext)}
                    className={`
                        flex items-center gap-1.5 px-2.5 py-1.5 rounded-full text-xs font-medium transition-all border shadow-sm
                        ${useHistoryContext 
                            ? 'bg-indigo-50 text-indigo-600 border-indigo-200 ring-1 ring-indigo-100' 
                            : 'bg-white text-slate-500 border-slate-200 hover:bg-slate-50 hover:text-slate-700'
                        }
                    `}
                >
                    <Icons.Layers className={`w-3.5 h-3.5 ${useHistoryContext ? 'text-indigo-500' : 'text-slate-400'}`} />
                    <span>携带画布上下文</span>
                </button>
                <span className="text-[10px] text-slate-400 ml-auto hidden sm:inline-block">
                    Press <kbd className="font-sans px-1 py-0.5 bg-slate-100 border border-slate-200 rounded text-slate-500">Ctrl/Command</kbd> + <kbd className="font-sans px-1 py-0.5 bg-slate-100 border border-slate-200 rounded text-slate-500">Enter</kbd>
                </span>
            </div>
            <div className="relative flex items-end gap-2 bg-slate-50 p-1.5 rounded-xl border border-slate-200 focus-within:border-indigo-300 focus-within:ring-4 focus-within:ring-indigo-50/50 focus-within:bg-white transition-all shadow-inner">
              <textarea
                value={inputValue}
                onChange={(e) => setInputValue(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder={isSending ? "AI 正在思考中..." : "输入您的问题，描述您的需求..."}
                disabled={isSending}
                className="flex-1 px-3 py-2 bg-transparent border-none focus:ring-0 text-sm text-slate-800 placeholder:text-slate-400 resize-none max-h-60 min-h-[50px] scrollbar-thin scrollbar-thumb-slate-200 scrollbar-track-transparent"
                rows={1}
                style={{ height: 'auto', minHeight: '50px' }}
              />
              <div className="flex gap-1 mb-0.5 shrink-0">
                  <button
                    onClick={handleSendMessage}
                    disabled={!inputValue.trim() || isSending}
                    className={`
                      p-2.5 rounded-lg transition-all duration-200 flex items-center justify-center
                      ${inputValue.trim() && !isSending
                        ? 'bg-indigo-600 text-white shadow-md shadow-indigo-200 hover:bg-indigo-700 hover:scale-105 active:scale-95' 
                        : 'bg-slate-200 text-slate-400 cursor-not-allowed'
                      }
                    `}
                  >
                    {isSending ? <Icons.Loader className="w-4 h-4" /> : <Icons.Send className="w-4 h-4" />}
                  </button>
                  <button
                    onClick={handleRestartSession}
                    className="p-2.5 rounded-lg bg-white text-slate-400 hover:bg-slate-50 hover:text-indigo-600 transition-all duration-200 border border-slate-200 hover:border-indigo-100 shadow-sm"
                    title="Restart Session"
                  >
                    <Icons.Plus className="w-4 h-4" />
                  </button>
              </div>
            </div>
            <div className="text-center mt-2.5">
                <p className="text-[10px] text-slate-400 font-medium">
                  {isSending ? 'AI is generating response...' : 'AI can make mistakes. Please verify important info.'}
                </p>
            </div>
          </div>
        </div>
      </div>

      {/* Export Modal - Polished */}
      {imgData && (
        <div className="absolute inset-0 bg-slate-900/60 backdrop-blur-sm flex items-center justify-center z-50 p-6 animate-in fade-in duration-200">
            <div className="bg-white p-0 rounded-2xl shadow-2xl max-h-[90vh] flex flex-col w-full max-w-4xl overflow-hidden animate-in zoom-in-95 duration-200 border border-white/20">
                <div className="flex justify-between items-center px-6 py-4 border-b border-slate-100 bg-slate-50/50">
                    <div className="flex items-center gap-3">
                        <div className="p-2 bg-green-100 text-green-600 rounded-lg">
                            <Icons.Download className="w-5 h-5" />
                        </div>
                        <div>
                            <h2 className="text-lg font-bold text-slate-800">Export Ready</h2>
                            <p className="text-xs text-slate-500">Your diagram has been successfully converted</p>
                        </div>
                    </div>
                    <button 
                        onClick={() => setImgData(null)}
                        className="p-2 text-slate-400 hover:text-slate-600 hover:bg-slate-100 rounded-full transition-colors"
                    >
                        <Icons.Close className="w-5 h-5" />
                    </button>
                </div>
                
                <div className="flex-1 overflow-auto bg-slate-50/50 p-8 flex items-center justify-center min-h-[400px]">
                    <div className="bg-white p-2 rounded shadow-sm border border-slate-200">
                        <img src={imgData} alt="Exported diagram" className="max-w-full h-auto object-contain" />
                    </div>
                </div>
                
                <div className="px-6 py-4 border-t border-slate-100 bg-white flex justify-end gap-3">
                   <button 
                        onClick={() => setImgData(null)}
                        className="px-5 py-2.5 text-slate-600 font-medium hover:bg-slate-100 rounded-lg transition-colors text-sm"
                    >
                        Close Preview
                    </button>
                    <a 
                        href={imgData} 
                        download="diagram.svg"
                        className="px-5 py-2.5 bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 shadow-lg shadow-indigo-200 hover:shadow-indigo-300 transition-all text-sm flex items-center gap-2"
                    >
                        <Icons.Download className="w-4 h-4" />
                        Download File
                    </a>
                </div>
            </div>
        </div>
      )}

      {/* Rename Modal */}
      {isRenameModalOpen && (
        <div className="absolute inset-0 bg-slate-900/60 backdrop-blur-sm flex items-center justify-center z-50 p-6 animate-in fade-in duration-200">
            <div className="bg-white rounded-2xl shadow-2xl w-full max-w-md overflow-hidden animate-in zoom-in-95 duration-200 border border-white/20">
                <div className="px-6 py-4 border-b border-slate-100 bg-slate-50/50 flex justify-between items-center">
                    <h2 className="text-lg font-bold text-slate-800">Rename Session</h2>
                    <button 
                        onClick={() => setIsRenameModalOpen(false)}
                        className="p-1 text-slate-400 hover:text-slate-600 hover:bg-slate-100 rounded-full transition-colors"
                    >
                        <Icons.Close className="w-5 h-5" />
                    </button>
                </div>
                
                <div className="p-6">
                    <label className="block text-sm font-medium text-slate-700 mb-2">
                        Session Name
                    </label>
                    <input 
                        type="text" 
                        value={newSessionTitle}
                        onChange={(e) => setNewSessionTitle(e.target.value)}
                        onKeyDown={(e) => e.key === 'Enter' && handleRenameSave()}
                        className="w-full px-4 py-2 border border-slate-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500 outline-none transition-all"
                        placeholder="Enter new name"
                        autoFocus
                    />
                </div>
                
                <div className="px-6 py-4 border-t border-slate-100 bg-slate-50/50 flex justify-end gap-3">
                   <button 
                        onClick={() => setIsRenameModalOpen(false)}
                        className="px-4 py-2 text-slate-600 font-medium hover:bg-slate-100 rounded-lg transition-colors text-sm"
                    >
                        Cancel
                    </button>
                    <button 
                        onClick={handleRenameSave}
                        className="px-4 py-2 bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 shadow-lg shadow-indigo-200 hover:shadow-indigo-300 transition-all text-sm"
                    >
                        Save Changes
                    </button>
                </div>
            </div>
        </div>
      )}
    </div>
  );
}

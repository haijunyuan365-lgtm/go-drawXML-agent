declare global {
  interface Window {
    __ENV?: {
      NEXT_PUBLIC_API_BASE_URL: string;
    };
  }
}

export {};

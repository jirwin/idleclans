import { useEffect, useRef, useCallback } from 'react';

interface UseSSEOptions {
  onUpdate: () => void;
  enabled?: boolean;
}

export function useSSE({ onUpdate, enabled = true }: UseSSEOptions) {
  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectTimeoutRef = useRef<number | null>(null);

  const connect = useCallback(() => {
    if (!enabled) return;
    
    // Clean up existing connection
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
    }

    const eventSource = new EventSource('/api/events');
    eventSourceRef.current = eventSource;

    eventSource.addEventListener('connected', () => {
      console.log('SSE connected');
    });

    eventSource.addEventListener('update', (event) => {
      console.log('SSE update:', event.data);
      onUpdate();
    });

    eventSource.onerror = () => {
      console.log('SSE connection error, reconnecting...');
      eventSource.close();
      
      // Reconnect after a delay
      if (reconnectTimeoutRef.current) {
        window.clearTimeout(reconnectTimeoutRef.current);
      }
      reconnectTimeoutRef.current = window.setTimeout(() => {
        connect();
      }, 5000);
    };
  }, [enabled, onUpdate]);

  useEffect(() => {
    connect();

    return () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close();
      }
      if (reconnectTimeoutRef.current) {
        window.clearTimeout(reconnectTimeoutRef.current);
      }
    };
  }, [connect]);

  return {
    reconnect: connect,
  };
}


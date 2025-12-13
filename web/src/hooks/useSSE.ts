import { useEffect, useRef, useCallback } from 'react';

interface UseSSEOptions {
  onUpdate: (eventType?: string) => void;
  enabled?: boolean;
}

export function useSSE({ onUpdate, enabled = true }: UseSSEOptions) {
  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectTimeoutRef = useRef<number | null>(null);
  
  // Store onUpdate in a ref so it doesn't cause reconnections when it changes
  const onUpdateRef = useRef(onUpdate);
  onUpdateRef.current = onUpdate;

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
      // Parse the event type from the data and pass it to the callback
      try {
        const data = JSON.parse(event.data);
        onUpdateRef.current(data.type);
      } catch {
        onUpdateRef.current();
      }
    });

    eventSource.onerror = () => {
      // Only log and reconnect if the connection was previously open
      // readyState: 0 = CONNECTING, 1 = OPEN, 2 = CLOSED
      if (eventSource.readyState === EventSource.CLOSED) {
        console.log('SSE connection closed, reconnecting...');
        eventSource.close();
        
        // Reconnect after a delay
        if (reconnectTimeoutRef.current) {
          window.clearTimeout(reconnectTimeoutRef.current);
        }
        reconnectTimeoutRef.current = window.setTimeout(() => {
          connect();
        }, 5000);
      }
      // If still CONNECTING, EventSource will retry automatically
    };
  }, [enabled]); // Removed onUpdate from dependencies

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


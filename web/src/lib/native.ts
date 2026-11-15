export interface NativeSource {
  id: string;
  name: string;
  width: number;
  height: number;
}

export interface NativeStatus {
  sessionId: string;
  fps: number;
  width: number;
  height: number;
  bitrate: number;
  audio: boolean;
  codec: string;
  error: string;
}

interface NativeCapture {
  Sources(): Promise<NativeSource[]>;
  Start(nick: string, token: string, sourceId: string, audio: boolean): Promise<string>;
  Stop(): Promise<void>;
  Status(): Promise<NativeStatus>;
}

declare global {
  interface Window {
    go?: { main?: { CaptureController?: NativeCapture } };
  }
}

export function nativeCapture(): NativeCapture | null {
  return window.go?.main?.CaptureController ?? null;
}

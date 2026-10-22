export interface Identity {
  nickname: string;
  token: string;
}

export interface NativeCapabilities {
  available: boolean;
  platform: string;
  reason?: string;
  audio: boolean;
  applicationAudio: boolean;
}

export interface NativeSource {
  id: string;
  name: string;
  kind: 'window' | 'monitor';
  width: number;
  height: number;
  processId?: number;
}

export interface NativeOptions {
  sourceId: string;
  audioSourceId: string;
  encoder: string;
}

export interface NativeStatus {
  phase: 'idle' | 'starting' | 'live';
  sessionId?: string;
  nickname?: string;
  error?: string;
  encoder?: string;
  stats: StreamStats;
}

export interface ServerConfig {
  publicUrl: string;
  iceServers: RTCIceServer[];
  maxViewers: number;
  addresses: string[];
  nativeCapture?: NativeCapabilities;
}

export interface ChannelStatus {
  nickname: string;
  exists: boolean;
  live: boolean;
  viewers: number;
  sessionId: string;
}

export interface StreamSettings {
  fps: number;
  bitrate: number;
  audio: 'window' | 'system' | 'off';
}

export interface Negotiation {
  description: RTCSessionDescriptionInit;
  sessionId: string;
  viewerId?: string;
}

export interface StreamStats {
  fps?: number;
  bitrate?: number;
  width?: number;
  height?: number;
  rtt?: number;
  codec?: string;
}

export interface Identity {
  nickname: string;
  token: string;
}

export interface ServerConfig {
  publicUrl: string;
  localUrl: string;
  iceServers: RTCIceServer[];
  maxViewers: number;
  maxVideoBitrate: number;
  addresses: string[];
}

export interface ChannelStatus {
  nickname: string;
  exists: boolean;
  live: boolean;
  viewers: number;
  sessionId: string;
}

export interface StreamSettings {
  videoPriority: 'resolution' | 'motion';
  audioSource: 'window' | 'system';
  sourceId: string;
  includeAudio: boolean;
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

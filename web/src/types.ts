export interface Identity { nickname: string; token: string }
export interface ServerConfig { publicUrl: string; iceServers: RTCIceServer[]; maxViewers: number; addresses: string[] }
export interface ChannelStatus { nickname: string; exists: boolean; live: boolean; viewers: number; sessionId: string }
export interface StreamSettings { fps: number; bitrate: number; audio: 'window' | 'system' | 'off' }
export interface Negotiation { description: RTCSessionDescriptionInit; sessionId: string; viewerId?: string }
export interface StreamStats { fps?: number; bitrate?: number; width?: number; height?: number; rtt?: number; codec?: string }

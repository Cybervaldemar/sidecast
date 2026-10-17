// Windows-only WASAPI adapter. PCM stays in native child-process pipes and is
// encoded by FFmpeg; it never travels through the web page or its JavaScript.
using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Runtime.InteropServices;
using System.Threading;

[ComImport, Guid("1CB9AD4C-DBFA-4C32-B178-C2F568A703B2"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IAudioClient {
    [PreserveSig] int Initialize(int mode, uint flags, long duration, long period, ref WaveFormat format, IntPtr session);
    [PreserveSig] int GetBufferSize(out uint frames);
    [PreserveSig] int GetStreamLatency(out long latency);
    [PreserveSig] int GetCurrentPadding(out uint frames);
    [PreserveSig] int IsFormatSupported(int mode, IntPtr format, out IntPtr closest);
    [PreserveSig] int GetMixFormat(out IntPtr format);
    [PreserveSig] int GetDevicePeriod(out long normal, out long minimum);
    [PreserveSig] int Start();
    [PreserveSig] int Stop();
    [PreserveSig] int Reset();
    [PreserveSig] int SetEventHandle(IntPtr handle);
    [PreserveSig] int GetService(ref Guid iid, [MarshalAs(UnmanagedType.IUnknown)] out object service);
}
[ComImport, Guid("C8ADBD64-E71E-48A0-A4DE-185C395CD317"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IAudioCaptureClient {
    [PreserveSig] int GetBuffer(out IntPtr data, out uint frames, out uint flags, out ulong position, out ulong qpc);
    [PreserveSig] int ReleaseBuffer(uint frames);
    [PreserveSig] int GetNextPacketSize(out uint frames);
}
[StructLayout(LayoutKind.Sequential, Pack=2)]
struct WaveFormat { public ushort Tag, Channels; public uint Rate, BytesPerSecond; public ushort BlockAlign, Bits, Extra; }
[StructLayout(LayoutKind.Sequential)]
struct ActivationParams { public uint Type, ProcessId, Mode; }
[StructLayout(LayoutKind.Explicit, Size=24)]
struct PropVariant { [FieldOffset(0)] public ushort Type; [FieldOffset(8)] public uint Size; [FieldOffset(16)] public IntPtr Data; }
[ComImport, Guid("72A22D78-CDE4-431D-B8CC-843A71199B6D"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IActivateAudioInterfaceAsyncOperation { [PreserveSig] int GetActivateResult(out int result, [MarshalAs(UnmanagedType.IUnknown)] out object instance); }
[ComVisible(true), Guid("41D949AB-9862-444A-80F6-C261334DA5EB"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IActivateAudioInterfaceCompletionHandler { [PreserveSig] int ActivateCompleted(IActivateAudioInterfaceAsyncOperation operation); }
[ComVisible(true), Guid("94EA2B94-E9CC-49E0-C0FF-EE64CA8F5B90"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IAgileObject {}
[ComVisible(true), ClassInterface(ClassInterfaceType.None)]
public sealed class Completion : IActivateAudioInterfaceCompletionHandler, IAgileObject, ICustomQueryInterface, IDisposable {
    [DllImport("ole32.dll")] static extern int CoCreateFreeThreadedMarshaler(IntPtr outer, out IntPtr marshaler);
    IntPtr marshaler;
    public Completion() {
        IntPtr outer=Marshal.GetIUnknownForObject(this);
        try { Marshal.ThrowExceptionForHR(CoCreateFreeThreadedMarshaler(outer,out marshaler)); }
        finally { Marshal.Release(outer); }
    }
    public CustomQueryInterfaceResult GetInterface(ref Guid iid,out IntPtr result) {
        result=IntPtr.Zero;
        if (iid == new Guid("00000003-0000-0000-C000-000000000046") && marshaler != IntPtr.Zero) {
            return Marshal.QueryInterface(marshaler,ref iid,out result) >= 0 ? CustomQueryInterfaceResult.Handled : CustomQueryInterfaceResult.Failed;
        }
        return CustomQueryInterfaceResult.NotHandled;
    }
    public void Dispose() { if (marshaler != IntPtr.Zero) { Marshal.Release(marshaler); marshaler=IntPtr.Zero; } Done.Dispose(); }
    public readonly ManualResetEvent Done = new ManualResetEvent(false);
    public object Instance;
    public int Result;
    public int ActivateCompleted(IActivateAudioInterfaceAsyncOperation operation) {
        try { int call = operation.GetActivateResult(out Result, out Instance); if (call < 0) Result = call; }
        catch (Exception error) { Result = Marshal.GetHRForException(error); }
        finally { Done.Set(); }
        return 0;
    }
}
[ComImport, Guid("A95664D2-9614-4F35-A746-DE8DB63617E6"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IMMDeviceEnumerator {
    [PreserveSig] int EnumAudioEndpoints(int flow, uint mask, out IntPtr devices);
    [PreserveSig] int GetDefaultAudioEndpoint(int flow, int role, out IMMDevice device);
}
[ComImport, Guid("D666063F-1587-4E43-81F1-B948E807363F"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IMMDevice { [PreserveSig] int Activate(ref Guid iid, uint context, IntPtr parameters, [MarshalAs(UnmanagedType.IUnknown)] out object instance); }

static class AudioCapture {
	[DllImport("ole32.dll")] static extern int CoInitializeEx(IntPtr reserved,uint mode);
    [DllImport("mfplat.dll")] static extern int MFStartup(uint version,uint flags);
    [DllImport("Mmdevapi.dll", ExactSpelling=true, CharSet=CharSet.Unicode)]
    static extern int ActivateAudioInterfaceAsync(string path, ref Guid iid, ref PropVariant parameters, IActivateAudioInterfaceCompletionHandler completion, out IActivateAudioInterfaceAsyncOperation operation);
    [DllImport("ntdll.dll", CharSet=CharSet.Unicode)] static extern int RtlGetVersion(ref OSVersion version);
    [StructLayout(LayoutKind.Sequential, CharSet=CharSet.Unicode)]
    struct OSVersion { public uint Size, Major, Minor, Build, Platform; [MarshalAs(UnmanagedType.ByValTStr,SizeConst=128)] public string ServicePack; }
    static readonly Guid ClientIID = new Guid("1CB9AD4C-DBFA-4C32-B178-C2F568A703B2");
    static void Check(int result) { if (result < 0) Marshal.ThrowExceptionForHR(result); }
    static uint Build() { var version = new OSVersion { Size=(uint)Marshal.SizeOf(typeof(OSVersion)) }; Check(RtlGetVersion(ref version)); return version.Build; }

    static IAudioClient OpenApplication(uint pid) {
        if (Build() < 20348) throw new NotSupportedException("Application audio requires Windows build 20348 or later.");
        var parameters = new ActivationParams { Type=1, ProcessId=pid, Mode=0 }; // include this process and its children
        IntPtr blob = Marshal.AllocHGlobal(Marshal.SizeOf(parameters));
        var completion = new Completion();
        IActivateAudioInterfaceAsyncOperation operation = null;
        try {
            Marshal.StructureToPtr(parameters, blob, false);
            var variant = new PropVariant { Type=65, Size=(uint)Marshal.SizeOf(parameters), Data=blob }; // VT_BLOB
            Guid iid = ClientIID;
            Check(ActivateAudioInterfaceAsync("VAD\\Process_Loopback", ref iid, ref variant, completion, out operation));
            if (!completion.Done.WaitOne(10000)) throw new TimeoutException("WASAPI application activation timed out.");
            Check(completion.Result);
            return (IAudioClient)completion.Instance;
        } finally {
            // On a timeout the process exits; do not leave a live COM callback
            // referring to an object whose activation memory has been released.
            if (operation != null && !completion.Done.WaitOne(0)) { Console.Error.WriteLine("WASAPI activation did not complete."); Environment.Exit(2); }
            if (operation != null) Marshal.ReleaseComObject(operation);
            Marshal.FreeHGlobal(blob);
            GC.KeepAlive(completion);
            completion.Dispose();
        }
    }
    static IAudioClient OpenSystem() {
        object enumerator = Activator.CreateInstance(Type.GetTypeFromCLSID(new Guid("BCDE0395-E52F-467C-8E3D-C4579291692E")));
        IMMDevice device = null;
        try {
            Check(((IMMDeviceEnumerator)enumerator).GetDefaultAudioEndpoint(0, 0, out device));
            object instance; Guid iid=ClientIID;
            Check(device.Activate(ref iid, 23, IntPtr.Zero, out instance));
            return (IAudioClient)instance;
        } finally {
            if (device != null) Marshal.ReleaseComObject(device);
            Marshal.ReleaseComObject(enumerator);
        }
    }

    [MTAThread]
    static int Main(string[] args) {
        try {
            if (args.Length == 1 && args[0] == "--capabilities") {
                Console.WriteLine("{\"systemAudio\":true,\"applicationAudio\":"+(Build()>=20348?"true":"false")+"}");
                return 0;
            }
            uint pid=0;
            if (args.Length == 2 && args[0] == "--pid") { if (!uint.TryParse(args[1],out pid) || pid == 0) throw new ArgumentException("Invalid process id."); }
            else if (args.Length != 1 || args[0] != "--system") throw new ArgumentException("Use --pid PROCESS_ID or --system.");
            Check(CoInitializeEx(IntPtr.Zero,0));
            Check(MFStartup(0x00020070,1));
            IAudioClient client = pid == 0 ? OpenSystem() : OpenApplication(pid);
            IAudioCaptureClient capture = null;
            try {
                var format = new WaveFormat { Tag=1, Channels=2, Rate=48000, BytesPerSecond=192000, BlockAlign=4, Bits=16, Extra=0 };
                Check(client.Initialize(0, 0x88020000, 200000, 0, ref format, IntPtr.Zero)); // shared loopback + PCM conversion
                object service; Guid iid = new Guid("C8ADBD64-E71E-48A0-A4DE-185C395CD317");
                Check(client.GetService(ref iid,out service)); capture=(IAudioCaptureClient)service;
                Check(client.Start());
                Console.Error.WriteLine("WASAPI_READY");
                var queue = new Queue<byte[]>(); int buffered=0, headOffset=0;
                byte[] output = new byte[1920]; // 10 ms, stereo 48 kHz PCM16
                var clock = Stopwatch.StartNew(); long next=0;
                using (Stream stdout=Console.OpenStandardOutput()) {
                    while (true) {
                        uint packet; Check(capture.GetNextPacketSize(out packet));
                        while (packet != 0) {
                            IntPtr data; uint frames,flags; ulong position,qpc;
                            Check(capture.GetBuffer(out data,out frames,out flags,out position,out qpc));
                            try {
                                byte[] copy = new byte[checked((int)frames*4)];
                                if ((flags & 2) == 0 && data != IntPtr.Zero) Marshal.Copy(data,copy,0,copy.Length);
                                queue.Enqueue(copy); buffered+=copy.Length;
                                while (buffered > 38400 && queue.Count > 1) { byte[] old=queue.Dequeue(); buffered-=old.Length-headOffset; headOffset=0; }
                            } finally { Check(capture.ReleaseBuffer(frames)); }
                            Check(capture.GetNextPacketSize(out packet));
                        }
                        if (clock.ElapsedMilliseconds >= next) {
                            Array.Clear(output,0,output.Length); int filled=0;
                            while (filled < output.Length && queue.Count != 0) {
                                byte[] head=queue.Peek(); int count=Math.Min(output.Length-filled,head.Length-headOffset);
                                Buffer.BlockCopy(head,headOffset,output,filled,count); headOffset+=count; filled+=count; buffered-=count;
                                if (headOffset == head.Length) { queue.Dequeue(); headOffset=0; }
                            }
                            stdout.Write(output,0,output.Length);
                            next+=10;
                            if (clock.ElapsedMilliseconds-next > 100) next=clock.ElapsedMilliseconds;
                        } else Thread.Sleep(1);
                    }
                }
            } finally {
                client.Stop();
                if (capture != null) Marshal.ReleaseComObject(capture);
                Marshal.ReleaseComObject(client);
            }
        } catch (Exception error) { Console.Error.WriteLine("Audio capture failed: "+error); return 1; }
    }
}

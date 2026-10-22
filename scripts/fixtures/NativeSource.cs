// Opt-in native integration-test source: its own animated window and quiet tone.
// The test captures this process only, never a user's application.
using System;
using System.Drawing;
using System.IO;
using System.Runtime.InteropServices;
using System.Windows.Forms;

sealed class NativeSource : Form {
    [DllImport("winmm.dll")] static extern bool PlaySound(IntPtr data, IntPtr module, uint flags);
    readonly Timer timer = new Timer();
    GCHandle wave;
    int frame;
    protected override bool ShowWithoutActivation { get { return true; } }
    NativeSource() {
        Text="Sidecast native integration source"; ClientSize=new Size(640,360);
        ShowInTaskbar=false; DoubleBuffered=true;
        timer.Interval=8; timer.Tick+=delegate { frame++; Invalidate(); }; timer.Start();
        using (var memory=new MemoryStream()) {
            var writer=new BinaryWriter(memory); int samples=48000;
            writer.Write(System.Text.Encoding.ASCII.GetBytes("RIFF")); writer.Write(36+samples*2);
            writer.Write(System.Text.Encoding.ASCII.GetBytes("WAVEfmt ")); writer.Write(16); writer.Write((short)1); writer.Write((short)1);
            writer.Write(48000); writer.Write(96000); writer.Write((short)2); writer.Write((short)16);
            writer.Write(System.Text.Encoding.ASCII.GetBytes("data")); writer.Write(samples*2);
            for(int i=0;i<samples;i++) writer.Write((short)(160*Math.Sin(2*Math.PI*440*i/48000)));
            wave=GCHandle.Alloc(memory.ToArray(),GCHandleType.Pinned);
        }
        PlaySound(wave.AddrOfPinnedObject(),IntPtr.Zero,0x0001|0x0004|0x0008); // async memory loop
    }
    protected override void OnPaint(PaintEventArgs e) {
        e.Graphics.Clear(Color.FromArgb(15,25,45));
        e.Graphics.FillRectangle(Brushes.LimeGreen,(frame*7)%560,60,80,240);
        e.Graphics.DrawString("Sidecast native test "+frame,Font,Brushes.White,20,20);
    }
    protected override void Dispose(bool disposing) { PlaySound(IntPtr.Zero,IntPtr.Zero,0); timer.Dispose(); if(wave.IsAllocated) wave.Free(); base.Dispose(disposing); }
    [STAThread] static void Main() { Application.Run(new NativeSource()); }
}

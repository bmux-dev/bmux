package graphics

import (
	"bytes"
	"image/color"
	"testing"
)

// FuzzKittyCommands runs a stream of Kitty graphics commands, as any program
// in a pane can send, separated by NUL bytes. Handle must never panic, and a
// pane and the store must never hold more image bytes than their caps.
func FuzzKittyCommands(f *testing.F) {
	png := b64(testPNG(f, 2, 2, color.NRGBA{R: 255, A: 255}))
	for _, seed := range []string{
		"a=T,f=100,i=1,U=1,c=2,r=1;" + png,
		"a=t,f=100,i=2,m=1;" + png[:8] + "\x00m=1;" + png[8:16] + "\x00m=0;" + png[16:],
		"a=t,f=24,s=1,v=1,i=3;AAAA\x00a=p,U=1,i=3\x00a=d,d=I,i=3",
		"a=q,i=31,s=1,v=1,t=d,f=24;AAAA",
		"a=t,f=32,s=65535,v=65535,i=4;AAAA",
		"a=t,f=100,i=16777215,q=2;" + png + "\x00a=d,d=A",
		"a=p,U=1,i=9,p=5,c=0,r=0",
	} {
		f.Add([]byte(seed))
	}
	limits := Limits{Pane: 4 << 10, Total: 6 << 10}
	f.Fuzz(func(t *testing.T, data []byte) {
		store := NewStore(limits)
		a, b := store.NewPane(Hooks{}), store.NewPane(Hooks{})
		for i, command := range bytes.Split(data, []byte{0}) {
			control, _, _ := bytes.Cut(command, []byte{';'})
			// File and shared-memory media read (and t=t deletes) real files.
			if bytes.Contains(control, []byte("t=f")) || bytes.Contains(control, []byte("t=t")) || bytes.Contains(control, []byte("t=s")) {
				continue
			}
			p := a
			if i%2 == 1 {
				p = b
			}
			_ = p.Handle(command)
			for _, pane := range []*Pane{a, b} {
				if pane.bytes > limits.Pane {
					t.Fatalf("a pane holds %d bytes, over its %d cap", pane.bytes, limits.Pane)
				}
			}
			store.mu.Lock()
			total := store.total
			store.mu.Unlock()
			if total > limits.Total {
				t.Fatalf("the store holds %d bytes, over its %d cap", total, limits.Total)
			}
		}
	})
}

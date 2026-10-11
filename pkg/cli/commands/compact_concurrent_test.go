package commands

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func TestCompactWarnings_Concurrent(t *testing.T) {
	proc := &compactProc{fixedNames: []string{"dept", "city"}}
	const workers = 64
	start := make(chan struct{})
	errs := make(chan error, workers)
	output := captureStderr(t, func() {
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p := packet.NewDataPacket(packet.TypeReference, "t")
				p.Schema = packet.Schema{Fields: []packet.Field{{Name: "dept", Type: "TEXT"}, {Name: "city", Type: "TEXT"}}}
				p.SetRows([][]string{{"Sales", "London"}, {"", ""}})
				<-start
				if err := proc.ProcessPacket(context.Background(), p); err != nil {
					errs <- err
					return
				}
				if p.Schema.Fields[0].Fixed || p.Schema.Fields[1].Fixed {
					t.Error("empty fields remained fixed")
				}
			}()
		}
		close(start)
		wg.Wait()
	})
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	for _, name := range []string{"dept", "city"} {
		if n := strings.Count(output, "compact: "+name+" is not fixed"); n != 1 {
			t.Errorf("%s warnings=%d, want 1", name, n)
		}
	}
}

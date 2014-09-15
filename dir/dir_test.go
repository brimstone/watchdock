package dir

import (
	"github.com/brimstone/go-chan-test/channel"
	"os"
	"testing"
	"time"
)

func Test(t *testing.T) {
	t.Log("Creating new watcher on /tmp")
	dir, err := New("/tmp")
	if err != nil {
		t.Error("Couldn't create a new watcher on /tmp")
	}

	// make a new channel to catch signals
	fileChannel := make(chan channel.File)
	t.Log("Running Sync()")
	go dir.Sync(fileChannel)

	t.Log("Creating output file")
	// open output file
	fo, err := os.Create("/tmp/output.txt")
	if err != nil {
		panic(err)
	}

	t.Log("Delaying write operation")
	go func() {
		// Wait a second
		time.Sleep(time.Second)
		// Write the file
		fo.Write([]byte("string"))
		// Close up, we're done here
		fo.Close()
	}()

	t.Log("Waiting for file to change")
OuterLoop:
	for {
		select {
		case event := <-fileChannel:
			t.Log("Got event about:", event.Filename)
			if event.Filename == "/tmp/output.txt" {
				t.Log("Passing notification back in")
				fileChannel <- event
				break OuterLoop
			}
		case <-time.After(2 * time.Second):
			t.Error("Timeout waiting for modify event")
		}
	}

	t.Log("Removing file")
	os.Remove("/tmp/output.txt")
OuterLoopB:
	for {
		select {
		case event := <-fileChannel:
			if event.Filename == "/tmp/output.txt" {
				break OuterLoopB
			} else {
				t.Log("File changed is not output.txt it's %v", event.Filename)
			}
		case <-time.After(2 * time.Second):
			t.Error("Timeout waiting for remove event")
		}
	}

}

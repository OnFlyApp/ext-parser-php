package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
)

// batchProtocol is written in the ready line. The host looks for these bytes
// in the .wasm before it starts the plugin in batch mode, so a plugin without
// them keeps being run once per file.
const batchProtocol = "onfly-batch/1"

// batchMemoryLimit is where the collector starts to work hard, below the
// 512 MB the host allows.
const batchMemoryLimit = 400 << 20

type batchRequest struct {
	Path    string `json:"path"`
	Content []byte `json:"content"` // base64 in JSON, so any bytes survive
}

type batchResponse struct {
	Chunks []Chunk `json:"chunks,omitempty"`
	Error  string  `json:"error,omitempty"`
}

// serveBatch answers one request per input line with one output line until the
// input ends. The grammar is decoded once for the whole run instead of once
// per file.
func serveBatch(in io.Reader, out io.Writer) error {
	// The host gives an instance 512 MB. Without a limit the collector lets
	// garbage from earlier files grow until an allocation fails.
	debug.SetMemoryLimit(batchMemoryLimit)
	r := bufio.NewReaderSize(in, 1<<20)
	w := bufio.NewWriter(out)
	enc := json.NewEncoder(w)
	if err := enc.Encode(map[string]string{"ready": batchProtocol}); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var req batchRequest
			var resp batchResponse
			if jerr := json.Unmarshal(line, &req); jerr != nil {
				resp.Error = fmt.Sprintf("bad request: %v", jerr)
			} else if chunks, perr := parse(req.Path, req.Content); perr != nil {
				resp.Error = perr.Error()
			} else {
				resp.Chunks = chunks
			}
			// The collector does not keep up on its own in a single-threaded
			// wasm instance; the live heap is small, so a full pass is cheap.
			runtime.GC()
			if eerr := enc.Encode(resp); eerr != nil {
				return eerr
			}
			if ferr := w.Flush(); ferr != nil {
				return ferr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func runBatch() {
	if err := serveBatch(os.Stdin, os.Stdout); err != nil {
		fatalError(err)
	}
}

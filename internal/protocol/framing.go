package protocol

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

var ErrFrameTooLarge = errors.New("request exceeds maximum frame size")

// Read a newline-framed request without letting json.Decoder grow without bound.
// Idle clients are allowed indefinitely, but a frame that has begun must finish.
type RequestReader struct {
	conn    net.Conn
	reader  *bufio.Reader
	timeout time.Duration
	initial bool
}

func NewRequestReader(conn net.Conn, timeout time.Duration) *RequestReader {
	return &RequestReader{conn: conn, reader: bufio.NewReaderSize(conn, 32<<10), timeout: timeout, initial: true}
}
func (r *RequestReader) Read() (Request, error) {
	if r.initial {
		_ = r.conn.SetReadDeadline(time.Now().Add(r.timeout))
		r.initial = false
	} else {
		_ = r.conn.SetReadDeadline(time.Time{})
	}
	first, err := r.reader.ReadByte()
	if err != nil {
		return Request{}, err
	}
	_ = r.conn.SetReadDeadline(time.Now().Add(r.timeout))
	data := make([]byte, 1, 4096)
	data[0] = first
	for first != '\n' {
		part, err := r.reader.ReadSlice('\n')
		if len(data)+len(part) > MaxRequestBytes {
			return Request{}, ErrFrameTooLarge
		}
		data = append(data, part...)
		if err == nil {
			break
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return Request{}, err
		}
	}
	_ = r.conn.SetReadDeadline(time.Time{})
	var request Request
	if err = json.Unmarshal(data, &request); err != nil {
		return request, fmt.Errorf("invalid request: %w", err)
	}
	if len(request.Text) > MaxInputBytes {
		return request, fmt.Errorf("input exceeds %d-byte limit", MaxInputBytes)
	}
	if len(request.Name) > 80 || len(request.NewName) > 80 || len(request.Dir) > 4096 {
		return request, fmt.Errorf("request field exceeds limit")
	}
	return request, nil
}

// Send one logical paste, using bounded transport chunks when needed. The
// server assembles and validates before admitting it to the PTY input queue.
func EncodeRequest(encoder *json.Encoder, r Request) error {
	if r.Op != "paste" {
		return encoder.Encode(r)
	}
	if len(r.Text) > MaxInputBytes {
		return fmt.Errorf("paste exceeds %d-byte limit", MaxInputBytes)
	}
	if len(r.Text) <= PasteChunkBytes {
		return encoder.Encode(r)
	}
	begin := Request{Op: "paste-begin", PaneID: r.PaneID, Size: len(r.Text)}
	if err := encoder.Encode(begin); err != nil {
		return err
	}
	for offset := 0; offset < len(r.Text); {
		end := min(offset+PasteChunkBytes, len(r.Text))
		// JSON strings must not split a UTF-8 sequence between requests.
		for end < len(r.Text) && end > offset && (r.Text[end]&0xc0) == 0x80 {
			end--
		}
		if end == offset {
			return fmt.Errorf("invalid paste encoding")
		}
		if err := encoder.Encode(Request{Op: "paste-chunk", Text: r.Text[offset:end]}); err != nil {
			return err
		}
		offset = end
	}
	return encoder.Encode(Request{Op: "paste-end"})
}

type PasteAssembly struct {
	active   bool
	expected int
	paneID   int
	text     []byte
}

func (p *PasteAssembly) Accept(r Request) (Request, bool, error) {
	switch r.Op {
	case "paste-begin":
		if p.active {
			return Request{}, false, fmt.Errorf("paste already in progress")
		}
		if r.Size < 0 || r.Size > MaxInputBytes {
			return Request{}, false, fmt.Errorf("paste exceeds input limit")
		}
		p.active, p.expected, p.paneID, p.text = true, r.Size, r.PaneID, make([]byte, 0, r.Size)
		return Request{}, false, nil
	case "paste-chunk":
		if !p.active {
			return Request{}, false, fmt.Errorf("no paste in progress")
		}
		if len(r.Text) > PasteChunkBytes || len(p.text)+len(r.Text) > p.expected {
			p.active = false
			p.text = nil
			return Request{}, false, fmt.Errorf("invalid paste chunk")
		}
		p.text = append(p.text, r.Text...)
		return Request{}, false, nil
	case "paste-end":
		if !p.active || len(p.text) != p.expected {
			p.active = false
			p.text = nil
			return Request{}, false, fmt.Errorf("incomplete paste")
		}
		complete := Request{Op: "paste", PaneID: p.paneID, Text: string(p.text)}
		*p = PasteAssembly{}
		return complete, true, nil
	default:
		if p.active {
			return Request{}, false, fmt.Errorf("paste must finish before another operation")
		}
		return r, true, nil
	}
}

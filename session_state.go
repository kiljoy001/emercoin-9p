package emercoin9p

import "sync"

// CommandResult holds the result of a command execution.
type CommandResult struct {
	Command string
	Result  string
	Error   error
}

type sessionState struct {
	mu       sync.RWMutex
	ctl      []byte
	data     []byte
	status   string
	results  chan CommandResult
	executor *commandExecutor
	server   *serverInfo
}

func newSessionState(config namespaceConfig) *sessionState {
	state := &sessionState{
		status:  "Ready",
		results: make(chan CommandResult, 16),
	}
	if config.backend != nil {
		state.executor = &commandExecutor{
			backend: config.backend,
			timeout: config.commandTimeout,
		}
	}
	state.server = config.serverInfo
	return state
}

func (s *sessionState) clone() *sessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return &sessionState{
		ctl:      append([]byte(nil), s.ctl...),
		data:     append([]byte(nil), s.data...),
		status:   s.status,
		results:  make(chan CommandResult, 16),
		executor: s.executor,
		server:   s.server,
	}
}

func (s *sessionState) setCtl(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctl = append(s.ctl[:0], data...)
}

func (s *sessionState) ctlSnapshot() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]byte(nil), s.ctl...)
}

func (s *sessionState) readCtl(offset uint64, count uint64) []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return readWindow(s.ctl, offset, count)
}

func (s *sessionState) setData(data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = []byte(data)
}

func (s *sessionState) getData() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return string(s.data)
}

func (s *sessionState) readData(offset uint64, count uint64) []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return readWindow(s.data, offset, count)
}

func (s *sessionState) setStatus(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

func (s *sessionState) getStatus() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *sessionState) readStatus(offset uint64, count uint64) []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return readWindow([]byte(s.status), offset, count)
}

func (s *sessionState) publishResult(result CommandResult) {
	select {
	case s.results <- result:
	default:
	}
}

func readWindow(src []byte, offset uint64, count uint64) []byte {
	if offset >= uint64(len(src)) {
		return []byte{}
	}

	start := int(offset)
	end := start + int(count)
	if end > len(src) {
		end = len(src)
	}

	out := make([]byte, end-start)
	copy(out, src[start:end])
	return out
}

func writeWindow(dst []byte, offset uint64, data []byte) []byte {
	start := int(offset)
	end := start + len(data)
	if end > len(dst) {
		dst = append(dst, make([]byte, end-len(dst))...)
	}
	copy(dst[start:end], data)
	return dst
}

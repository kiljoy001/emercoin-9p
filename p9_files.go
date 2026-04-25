package emercoin9p

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
)

type commandOpenState struct {
	mode   proto.Mode
	buffer []byte
}

// CommandFile represents the writable ctl file for a session.
type CommandFile struct {
	*fs.BaseFile
	state *sessionState
	mu    sync.Mutex
	opens map[uint64]*commandOpenState
}

var _ fs.File = (*CommandFile)(nil)

// NewCommandFile creates a new session command file.
func NewCommandFile(stat *proto.Stat, state *sessionState) *CommandFile {
	return &CommandFile{
		BaseFile: fs.NewBaseFile(stat),
		state:    state,
		opens:    make(map[uint64]*commandOpenState),
	}
}

// Open prepares a fid-local ctl buffer.
func (c *CommandFile) Open(fid uint64, omode proto.Mode) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	buffer := c.state.ctlSnapshot()
	if omode&proto.Otrunc != 0 {
		buffer = []byte{}
	}
	c.opens[fid] = &commandOpenState{
		mode:   omode,
		buffer: buffer,
	}
	return nil
}

// Read serves either the fid-local ctl buffer or the committed ctl content.
func (c *CommandFile) Read(fid uint64, offset uint64, count uint64) ([]byte, error) {
	c.mu.Lock()
	openState, ok := c.opens[fid]
	c.mu.Unlock()
	if ok {
		return readWindow(openState.buffer, offset, count), nil
	}
	return c.state.readCtl(offset, count), nil
}

// Write stores data in the fid-local buffer and processes it immediately.
func (c *CommandFile) Write(fid uint64, offset uint64, data []byte) (uint32, error) {
	c.mu.Lock()
	if openState, ok := c.opens[fid]; ok {
		openState.buffer = writeWindow(openState.buffer, offset, data)
		buffer := append([]byte(nil), openState.buffer...)
		c.mu.Unlock()
		c.commit(buffer)
		return uint32(len(data)), nil
	}
	c.mu.Unlock()

	buffer := writeWindow(c.state.ctlSnapshot(), offset, data)
	c.commit(buffer)
	return uint32(len(data)), nil
}

// Close releases any fid-local state for the ctl file.
func (c *CommandFile) Close(fid uint64) error {
	c.mu.Lock()
	delete(c.opens, fid)
	c.mu.Unlock()
	return nil
}

func (c *CommandFile) commit(buffer []byte) {
	c.state.setCtl(buffer)
	command := strings.TrimSpace(string(buffer))
	if command == "" {
		return
	}

	if result, ok := c.localCommandResult(command); ok {
		c.state.setData(result.Result)
		if result.Error != nil {
			c.state.setStatus(fmt.Sprintf("Error: %v", result.Error))
		} else {
			c.state.setStatus("OK!")
		}
		c.state.publishResult(result)
		return
	}

	if c.state.executor == nil {
		c.state.publishResult(CommandResult{Command: command})
		return
	}

	c.state.setStatus("Running")
	result, err := c.state.executor.Execute(context.Background(), command)
	if err != nil {
		c.state.setData("")
		c.state.setStatus(fmt.Sprintf("Error: %v", err))
		c.state.publishResult(result)
		return
	}

	c.state.setData(result.Result)
	c.state.setStatus("OK!")
	c.state.publishResult(result)
}

func (c *CommandFile) localCommandResult(command string) (CommandResult, bool) {
	switch strings.TrimSpace(command) {
	case "port", "server_port":
		if c.state.server == nil {
			return CommandResult{Command: command, Error: fmt.Errorf("server port is unavailable")}, true
		}
		port := c.state.server.Port()
		if port == 0 {
			return CommandResult{Command: command, Error: fmt.Errorf("server port is unavailable")}, true
		}
		return CommandResult{
			Command: command,
			Result:  fmt.Sprintf("%d", port),
		}, true
	default:
		return CommandResult{}, false
	}
}

// ReadAt is a convenience wrapper for local callers outside the 9P server.
func (c *CommandFile) ReadAt(buf []byte, offset int64) (int, error) {
	data, err := c.Read(0, uint64(offset), uint64(len(buf)))
	if err != nil {
		return 0, err
	}
	return copy(buf, data), nil
}

// WriteAt is a convenience wrapper for local callers outside the 9P server.
func (c *CommandFile) WriteAt(buf []byte, offset int64) (int, error) {
	n, err := c.Write(0, uint64(offset), buf)
	return int(n), err
}

// GetResult waits for a queued command result.
func (c *CommandFile) GetResult(ctx context.Context, timeout interface{}) (*CommandResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if duration, ok := timeout.(time.Duration); ok && duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, duration)
		defer cancel()
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-c.state.results:
		return &result, nil
	}
}

// DataFile represents the read-only data file for a session.
type DataFile struct {
	*fs.BaseFile
	state *sessionState
}

var _ fs.File = (*DataFile)(nil)

// NewDataFile creates a new session data file.
func NewDataFile(stat *proto.Stat, state *sessionState) *DataFile {
	return &DataFile{
		BaseFile: fs.NewBaseFile(stat),
		state:    state,
	}
}

// Read serves the current data content.
func (d *DataFile) Read(fid uint64, offset uint64, count uint64) ([]byte, error) {
	return d.state.readData(offset, count), nil
}

// Write rejects writes to the read-only data file.
func (d *DataFile) Write(fid uint64, offset uint64, data []byte) (uint32, error) {
	return 0, fmt.Errorf("data is read-only")
}

// Close is a no-op for the in-memory data file.
func (d *DataFile) Close(fid uint64) error {
	return nil
}

// SetData updates the data file content.
func (d *DataFile) SetData(data string) {
	d.state.setData(data)
}

// GetData returns the data file content.
func (d *DataFile) GetData() string {
	return d.state.getData()
}

// StatusFile represents the read-only status file for a session.
type StatusFile struct {
	*fs.BaseFile
	state *sessionState
}

var _ fs.File = (*StatusFile)(nil)

// NewStatusFile creates a new session status file.
func NewStatusFile(stat *proto.Stat, state *sessionState) *StatusFile {
	return &StatusFile{
		BaseFile: fs.NewBaseFile(stat),
		state:    state,
	}
}

// Read serves the current status content.
func (s *StatusFile) Read(fid uint64, offset uint64, count uint64) ([]byte, error) {
	return s.state.readStatus(offset, count), nil
}

// Write rejects writes to the read-only status file.
func (s *StatusFile) Write(fid uint64, offset uint64, data []byte) (uint32, error) {
	return 0, fmt.Errorf("status is read-only")
}

// Close is a no-op for the in-memory status file.
func (s *StatusFile) Close(fid uint64) error {
	return nil
}

// SetStatus updates the status file content.
func (s *StatusFile) SetStatus(status string) {
	s.state.setStatus(status)
}

// GetStatus returns the status file content.
func (s *StatusFile) GetStatus() string {
	return s.state.getStatus()
}

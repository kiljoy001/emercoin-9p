package emercoin9p

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/knusbaum/go9p"
	"github.com/knusbaum/go9p/client"
	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
)

type twoPipe struct {
	*io.PipeReader
	*io.PipeWriter
}

func (t *twoPipe) Close() error {
	_ = t.PipeReader.Close()
	_ = t.PipeWriter.Close()
	return nil
}

func openTestClient(t *testing.T, ns *Namespace) *client.Client {
	t.Helper()

	p1r, p1w := io.Pipe()
	p2r, p2w := io.Pipe()
	pipe := &twoPipe{PipeReader: p2r, PipeWriter: p1w}

	go func() {
		_ = go9p.ServeReadWriter(p1r, p2w, ns.Server())
	}()

	c, err := client.NewClient(pipe, "glenda", "")
	if err != nil {
		t.Fatalf("failed to create 9p client: %v", err)
	}

	t.Cleanup(func() {
		_ = pipe.Close()
	})

	return c
}

func mustChildDir(t *testing.T, parent fs.Dir, name string) fs.Dir {
	t.Helper()

	child, ok := parent.Children()[name]
	if !ok {
		t.Fatalf("directory %q not found", name)
	}

	dir, ok := child.(fs.Dir)
	if !ok {
		t.Fatalf("%q is not a directory", name)
	}

	return dir
}

func mustChildFile(t *testing.T, parent fs.Dir, name string) fs.File {
	t.Helper()

	child, ok := parent.Children()[name]
	if !ok {
		t.Fatalf("file %q not found", name)
	}

	file, ok := child.(fs.File)
	if !ok {
		t.Fatalf("%q is not a file", name)
	}

	return file
}

func readRemoteFile(t *testing.T, c *client.Client, path string) string {
	t.Helper()

	f, err := c.Open(path, proto.Oread)
	if err != nil {
		t.Fatalf("failed to open %s: %v", path, err)
	}
	defer func() {
		_ = f.Close()
	}()

	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}

	return string(data)
}

func writeRemoteFile(t *testing.T, c *client.Client, path string, data string) {
	t.Helper()

	f, err := c.Open(path, proto.Ordwr|proto.Otrunc)
	if err != nil {
		t.Fatalf("failed to open %s for write: %v", path, err)
	}
	defer func() {
		_ = f.Close()
	}()

	n, err := f.Write([]byte(data))
	if err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
	if n != len(data) {
		t.Fatalf("short write to %s: got %d want %d", path, n, len(data))
	}
}

func setupTestSession(t *testing.T) (*Namespace, *UserSession) {
	t.Helper()

	ns := NewNs()
	session, err := ns.CreateSession("test-user-1", "testuser")
	if err != nil {
		t.Fatalf("failed to create test session: %v", err)
	}

	return ns, session
}

func setupMultiuserTest(t *testing.T) (*Namespace, []*UserSession) {
	t.Helper()

	ns := NewNs()
	users := []struct {
		id   string
		name string
	}{
		{id: "user-1", name: "alice"},
		{id: "user-2", name: "bob"},
		{id: "user-3", name: "charlie"},
	}

	sessions := make([]*UserSession, 0, len(users))
	for _, user := range users {
		session, err := ns.CreateSession(user.id, user.name)
		if err != nil {
			t.Fatalf("failed to create %s session: %v", user.name, err)
		}
		sessions = append(sessions, session)
	}

	return ns, sessions
}

// TestServer9PFiles verifies the exported namespace is built from real go9p/fs nodes.
func TestServer9PFiles(t *testing.T) {
	ns, session := setupTestSession(t)

	if ns.Control() == nil {
		t.Fatalf("root control session was not created")
	}
	if _, ok := mustChildFile(t, ns.Root(), "ctl").(*CommandFile); !ok {
		t.Fatalf("root ctl is not backed by CommandFile")
	}
	if _, ok := mustChildFile(t, ns.Root(), "data").(*DataFile); !ok {
		t.Fatalf("root data is not backed by DataFile")
	}
	if _, ok := mustChildFile(t, ns.Root(), "status").(*StatusFile); !ok {
		t.Fatalf("root status is not backed by StatusFile")
	}

	sessionsDir := mustChildDir(t, ns.Root(), "sessions")
	userDir := mustChildDir(t, sessionsDir, "testuser")

	if _, ok := mustChildFile(t, userDir, "ctl").(*CommandFile); !ok {
		t.Fatalf("ctl is not backed by CommandFile")
	}
	if _, ok := mustChildFile(t, userDir, "data").(*DataFile); !ok {
		t.Fatalf("data is not backed by DataFile")
	}
	if _, ok := mustChildFile(t, userDir, "status").(*StatusFile); !ok {
		t.Fatalf("status is not backed by StatusFile")
	}
	if _, ok := mustChildFile(t, session.Namespace.Root(), "ctl").(*CommandFile); !ok {
		t.Fatalf("session namespace ctl is not backed by CommandFile")
	}
	if _, ok := mustChildFile(t, session.Namespace.Root(), "data").(*DataFile); !ok {
		t.Fatalf("session namespace data is not backed by DataFile")
	}
	if _, ok := mustChildFile(t, session.Namespace.Root(), "status").(*StatusFile); !ok {
		t.Fatalf("session namespace status is not backed by StatusFile")
	}

	ns.mu.RLock()
	defer ns.mu.RUnlock()
	if len(ns.sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(ns.sessions))
	}
}

func TestRootCtlPortCommand(t *testing.T) {
	ns := NewNs()
	ns.config.serverInfo.SetPort(4242)

	c := openTestClient(t, ns)

	writeRemoteFile(t, c, "/ctl", "port")

	if got := readRemoteFile(t, c, "/data"); got != "4242" {
		t.Fatalf("unexpected port data: %q", got)
	}
	if got := readRemoteFile(t, c, "/status"); got != "OK!" {
		t.Fatalf("unexpected port status: %q", got)
	}

	result, err := ns.Control().Ctl.GetResult(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("failed waiting for root ctl result: %v", err)
	}
	if result.Command != "port" {
		t.Fatalf("unexpected command result command: %q", result.Command)
	}
	if result.Result != "4242" {
		t.Fatalf("unexpected command result payload: %q", result.Result)
	}
}

// TestMultiuserIsolation verifies each session has isolated backing state.
func TestMultiuserIsolation(t *testing.T) {
	ns, sessions := setupMultiuserTest(t)

	alice := sessions[0]
	bob := sessions[1]
	charlie := sessions[2]

	alice.Data.SetData("alice's data")
	bob.Data.SetData("bob's data")
	charlie.Data.SetData("charlie's data")

	alice.SetStatus("Alice OK!")
	bob.SetStatus("Bob OK!")
	charlie.SetStatus("Charlie OK!")

	c := openTestClient(t, ns)

	if got := readRemoteFile(t, c, "/sessions/alice/data"); got != "alice's data" {
		t.Fatalf("alice data corrupted: %q", got)
	}
	if got := readRemoteFile(t, c, "/sessions/bob/data"); got != "bob's data" {
		t.Fatalf("bob data corrupted: %q", got)
	}
	if got := readRemoteFile(t, c, "/sessions/charlie/data"); got != "charlie's data" {
		t.Fatalf("charlie data corrupted: %q", got)
	}
	if got := readRemoteFile(t, c, "/sessions/alice/status"); got != "Alice OK!" {
		t.Fatalf("alice status corrupted: %q", got)
	}
	if got := readRemoteFile(t, c, "/sessions/bob/status"); got != "Bob OK!" {
		t.Fatalf("bob status corrupted: %q", got)
	}
	if got := readRemoteFile(t, c, "/sessions/charlie/status"); got != "Charlie OK!" {
		t.Fatalf("charlie status corrupted: %q", got)
	}

	if alice.Namespace == bob.Namespace {
		t.Fatalf("alice and bob share a namespace")
	}
	if alice.Namespace == charlie.Namespace {
		t.Fatalf("alice and charlie share a namespace")
	}
}

// TestClonePattern verifies cloned sessions copy state without sharing it.
func TestClonePattern(t *testing.T) {
	ns := NewNs()

	original, err := ns.CreateSession("original", "original")
	if err != nil {
		t.Fatalf("failed to create original session: %v", err)
	}

	original.Data.SetData("original data")
	original.SetStatus("OK!")

	clone, err := ns.CloneSession("original", "clone", "clone")
	if err != nil {
		t.Fatalf("failed to clone session: %v", err)
	}

	if clone.GetData() != "original data" {
		t.Fatalf("clone data mismatch: %q", clone.GetData())
	}
	if clone.GetStatus() != "OK!" {
		t.Fatalf("clone status mismatch: %q", clone.GetStatus())
	}

	clone.Data.SetData("clone data modified")
	clone.SetStatus("Clone OK!")

	if original.GetData() != "original data" {
		t.Fatalf("original data changed after clone modification: %q", original.GetData())
	}
	if original.GetStatus() != "OK!" {
		t.Fatalf("original status changed after clone modification: %q", original.GetStatus())
	}

	c := openTestClient(t, ns)
	if got := readRemoteFile(t, c, "/sessions/original/data"); got != "original data" {
		t.Fatalf("unexpected original data via 9p: %q", got)
	}
	if got := readRemoteFile(t, c, "/sessions/clone/data"); got != "clone data modified" {
		t.Fatalf("unexpected clone data via 9p: %q", got)
	}
}

// TestCommandFileWrite verifies ctl writes flow through the real 9P stack and queue commands.
func TestCommandFileWrite(t *testing.T) {
	ns, session := setupTestSession(t)
	c := openTestClient(t, ns)

	writeRemoteFile(t, c, "/sessions/testuser/ctl", "getbalance")

	if got := readRemoteFile(t, c, "/sessions/testuser/ctl"); got != "getbalance" {
		t.Fatalf("ctl content mismatch: %q", got)
	}

	result, err := session.Ctl.GetResult(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("failed waiting for command result: %v", err)
	}
	if result.Command != "getbalance" {
		t.Fatalf("queued command mismatch: %q", result.Command)
	}
}

// TestStatusFileRead verifies status reads go through the exported 9P tree.
func TestStatusFileRead(t *testing.T) {
	ns, session := setupTestSession(t)
	session.SetStatus("OK!")

	c := openTestClient(t, ns)
	if got := readRemoteFile(t, c, "/sessions/testuser/status"); got != "OK!" {
		t.Fatalf("status mismatch: %q", got)
	}
}

// TestDataFileRead verifies data reads go through the exported 9P tree.
func TestDataFileRead(t *testing.T) {
	ns, session := setupTestSession(t)
	session.Data.SetData("test result data")

	c := openTestClient(t, ns)
	if got := readRemoteFile(t, c, "/sessions/testuser/data"); got != "test result data" {
		t.Fatalf("data mismatch: %q", got)
	}
}

// TestDataFileIsReadOnly verifies read-only permissions are enforced by go9p.
func TestDataFileIsReadOnly(t *testing.T) {
	ns, _ := setupTestSession(t)
	c := openTestClient(t, ns)

	if _, err := c.Open("/sessions/testuser/data", proto.Owrite); err == nil {
		t.Fatalf("expected write open on data file to fail")
	}
}

// TestStatusFileIsReadOnly verifies read-only permissions are enforced by go9p.
func TestStatusFileIsReadOnly(t *testing.T) {
	ns, _ := setupTestSession(t)
	c := openTestClient(t, ns)

	if _, err := c.Open("/sessions/testuser/status", proto.Owrite); err == nil {
		t.Fatalf("expected write open on status file to fail")
	}
}

// TestSessionManagement verifies multiple sessions are registered and exported.
func TestSessionManagement(t *testing.T) {
	ns := NewNs()

	for i := 0; i < 5; i++ {
		if _, err := ns.CreateSession(fmt.Sprintf("user-%d", i), fmt.Sprintf("user%d", i)); err != nil {
			t.Fatalf("failed to create session %d: %v", i, err)
		}
	}

	ns.mu.RLock()
	defer ns.mu.RUnlock()

	if len(ns.sessions) != 5 {
		t.Fatalf("expected 5 sessions, got %d", len(ns.sessions))
	}

	sessionsDir := mustChildDir(t, ns.Root(), "sessions")
	if len(sessionsDir.Children()) != 5 {
		t.Fatalf("expected 5 exported session directories, got %d", len(sessionsDir.Children()))
	}

	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("user-%d", i)
		if _, ok := ns.sessions[id]; !ok {
			t.Fatalf("session %s not found", id)
		}
	}
}

// MockRPCBackend simulates an RPC backend for testing.
type MockRPCBackend struct {
	responses map[string]json.RawMessage
	errs      map[string]error
	queries   []Query
	mu        sync.RWMutex
}

// NewMockRPCBackend creates a new mock RPC backend.
func NewMockRPCBackend() *MockRPCBackend {
	return &MockRPCBackend{
		responses: make(map[string]json.RawMessage),
		errs:      make(map[string]error),
	}
}

// AddResponse adds a mock response for a command.
func (m *MockRPCBackend) AddResponse(cmd string, response json.RawMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[cmd] = response
}

// AddError adds a mock error for a command.
func (m *MockRPCBackend) AddError(cmd string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errs[cmd] = err
}

// Send implements RPCSend for tests.
func (m *MockRPCBackend) Send(ctx context.Context, query Query) (json.RawMessage, error) {
	_ = ctx

	m.mu.Lock()
	defer m.mu.Unlock()

	m.queries = append(m.queries, Query{
		Cmd:  query.Cmd,
		Args: append([]any(nil), query.Args...),
	})

	if err, ok := m.errs[query.Cmd]; ok {
		return nil, err
	}
	if resp, ok := m.responses[query.Cmd]; ok {
		return resp, nil
	}

	return nil, fmt.Errorf("unknown command: %s", query.Cmd)
}

func (m *MockRPCBackend) LastQuery() Query {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.queries) == 0 {
		return Query{}
	}
	return m.queries[len(m.queries)-1]
}

// TestMockRPCBackend verifies ctl writes are executed through the configured RPC backend.
func TestMockRPCBackend(t *testing.T) {
	backend := NewMockRPCBackend()
	backend.AddResponse("name_show", json.RawMessage(`{"name":"dns:rentonsoftworks.coin","value":"1.2.3.4"}`))

	ns := NewNs(WithRPCBackend(backend))
	session, err := ns.CreateSession("test-user-1", "testuser")
	if err != nil {
		t.Fatalf("failed to create test session: %v", err)
	}
	c := openTestClient(t, ns)

	writeRemoteFile(t, c, "/sessions/testuser/ctl", "name_show dns:rentonsoftworks.coin")

	result, err := session.Ctl.GetResult(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("failed waiting for ctl command: %v", err)
	}

	if session.GetStatus() != "OK!" {
		t.Fatalf("status not updated correctly: %q", session.GetStatus())
	}

	lastQuery := backend.LastQuery()
	if lastQuery.Cmd != "name_show" {
		t.Fatalf("unexpected RPC method: %q", lastQuery.Cmd)
	}
	if len(lastQuery.Args) != 1 || lastQuery.Args[0] != "dns:rentonsoftworks.coin" {
		t.Fatalf("unexpected RPC args: %#v", lastQuery.Args)
	}

	const expectedData = `{"name":"dns:rentonsoftworks.coin","value":"1.2.3.4"}`
	if got := readRemoteFile(t, c, "/sessions/testuser/data"); got != expectedData {
		t.Fatalf("data mismatch: got %q want %q", got, expectedData)
	}
	if got := readRemoteFile(t, c, "/sessions/testuser/status"); got != "OK!" {
		t.Fatalf("status mismatch via 9p: %q", got)
	}
	if result.Command != "name_show" {
		t.Fatalf("unexpected queued command: %q", result.Command)
	}
	if result.Result != expectedData {
		t.Fatalf("unexpected queued result: %q", result.Result)
	}
	if result.Error != nil {
		t.Fatalf("unexpected queued error: %v", result.Error)
	}
}

func TestMockRPCBackendError(t *testing.T) {
	backend := NewMockRPCBackend()
	backend.AddError("broken", errors.New("rpc failed"))

	ns := NewNs(WithRPCBackend(backend))
	session, err := ns.CreateSession("test-user-1", "testuser")
	if err != nil {
		t.Fatalf("failed to create test session: %v", err)
	}
	c := openTestClient(t, ns)

	writeRemoteFile(t, c, "/sessions/testuser/ctl", "broken")

	result, err := session.Ctl.GetResult(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("failed waiting for ctl command: %v", err)
	}

	if got := readRemoteFile(t, c, "/sessions/testuser/data"); got != "" {
		t.Fatalf("expected empty data on rpc failure, got %q", got)
	}
	if got := readRemoteFile(t, c, "/sessions/testuser/status"); got != "Error: rpc failed" {
		t.Fatalf("unexpected error status: %q", got)
	}
	if result.Command != "broken" {
		t.Fatalf("unexpected queued command: %q", result.Command)
	}
	if result.Error == nil || result.Error.Error() != "rpc failed" {
		t.Fatalf("unexpected queued error: %v", result.Error)
	}
}

// TestConcurrentSessionAccess verifies file implementations remain safe under concurrent use.
func TestConcurrentSessionAccess(t *testing.T) {
	_, sessions := setupMultiuserTest(t)

	for _, session := range sessions {
		session.Data.SetData(fmt.Sprintf("%s-data", session.Name))
		session.SetStatus(fmt.Sprintf("%s-status", session.Name))
	}

	var wg sync.WaitGroup
	errChan := make(chan error, 32)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			session := sessions[idx%len(sessions)]
			cmd := fmt.Sprintf("command-%d", idx)
			fidBase := uint64(idx+1) * 16

			if err := session.Ctl.Open(fidBase, proto.Ordwr|proto.Otrunc); err != nil {
				errChan <- fmt.Errorf("ctl open failed: %w", err)
				return
			}
			if n, err := session.Ctl.Write(fidBase, 0, []byte(cmd)); err != nil {
				errChan <- fmt.Errorf("ctl write failed: %w", err)
				return
			} else if int(n) != len(cmd) {
				errChan <- fmt.Errorf("ctl short write: got %d want %d", n, len(cmd))
				return
			}
			if err := session.Ctl.Close(fidBase); err != nil {
				errChan <- fmt.Errorf("ctl close failed: %w", err)
				return
			}

			if err := session.Data.Open(fidBase+1, proto.Oread); err != nil {
				errChan <- fmt.Errorf("data open failed: %w", err)
				return
			}
			data, err := session.Data.Read(fidBase+1, 0, 128)
			_ = session.Data.Close(fidBase + 1)
			if err != nil {
				errChan <- fmt.Errorf("data read failed: %w", err)
				return
			}
			if string(data) != fmt.Sprintf("%s-data", session.Name) {
				errChan <- fmt.Errorf("data mismatch for %s: %q", session.Name, string(data))
				return
			}

			if err := session.Status.Open(fidBase+2, proto.Oread); err != nil {
				errChan <- fmt.Errorf("status open failed: %w", err)
				return
			}
			status, err := session.Status.Read(fidBase+2, 0, 128)
			_ = session.Status.Close(fidBase + 2)
			if err != nil {
				errChan <- fmt.Errorf("status read failed: %w", err)
				return
			}
			if string(status) != fmt.Sprintf("%s-status", session.Name) {
				errChan <- fmt.Errorf("status mismatch for %s: %q", session.Name, string(status))
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("concurrent access error: %v", err)
	}
}
